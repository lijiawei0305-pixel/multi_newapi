package mtwire

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPayableEarningRestartAfterCreditBeforeStatusUpdate(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	entry := agent.EarningEntry{
		TenantID:   7,
		UserID:     11,
		SourceType: agent.SourceConsumeCommission,
		SourceID:   "restart-after-credit",
		Amount:     2.75,
		Remark:     "durable handoff",
	}
	row, err := app.ensurePayableEarningIntent(ctx, entry)
	require.NoError(t, err)

	const callbackName = "test:fail_payable_status_update"
	require.NoError(t, app.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == row.TableName() {
			tx.AddError(errors.New("simulated crash before payable status commit"))
		}
	}))
	require.Error(t, app.applyPayableEarningIntent(ctx, row.IdemKey))
	require.NoError(t, app.DB.Callback().Update().Remove(callbackName))

	var pending payableEarningIntentRow
	require.NoError(t, app.DB.Where("idem_key = ?", row.IdemKey).First(&pending).Error)
	assert.Equal(t, payableEarningStatusPending, pending.Status)
	wallet, err := app.AgentRepo.GetWallet(ctx, entry.TenantID)
	require.NoError(t, err)
	assert.Equal(t, entry.Amount, wallet.WithdrawableBalance)

	result, err := app.reconcilePayableEarningIntents(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Scanned)
	assert.Equal(t, 1, result.Applied)
	assert.Zero(t, result.Failed)
	assert.Zero(t, result.Pending)

	wallet, err = app.AgentRepo.GetWallet(ctx, entry.TenantID)
	require.NoError(t, err)
	assert.Equal(t, entry.Amount, wallet.WithdrawableBalance, "replay must not credit the idempotent ledger twice")
	assert.Equal(t, entry.Amount, wallet.TotalEarned)
	var earningCount int64
	require.NoError(t, app.DB.Table("agent_earning_logs").
		Where("tenant_id = ? AND source_type = ? AND source_id = ?", entry.TenantID, entry.SourceType, entry.SourceID).
		Count(&earningCount).Error)
	assert.Equal(t, int64(1), earningCount)
}

type selectiveFailingEarningSink struct {
	failSourceID string
	delegate     agent.EarningSink
}

func (s selectiveFailingEarningSink) AddEarning(ctx context.Context, entry agent.EarningEntry) error {
	if entry.SourceID == s.failSourceID {
		return errors.New("poison earning")
	}
	return s.delegate.AddEarning(ctx, entry)
}

func TestPayableEarningPoisonRowDoesNotBlockLaterIntent(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	baseSink := app.AgentEarnings
	app.AgentEarnings = selectiveFailingEarningSink{failSourceID: "poison", delegate: baseSink}

	poison := agent.EarningEntry{
		TenantID:   8,
		UserID:     12,
		SourceType: agent.SourceRatioMarkup,
		SourceID:   "poison",
		Amount:     1.25,
	}
	healthy := poison
	healthy.SourceID = "healthy"
	healthy.Amount = 3.50
	_, err := app.ensurePayableEarningIntent(ctx, poison)
	require.NoError(t, err)
	_, err = app.ensurePayableEarningIntent(ctx, healthy)
	require.NoError(t, err)

	result, err := app.reconcilePayableEarningIntents(ctx, 10)
	require.Error(t, err)
	assert.Equal(t, 2, result.Scanned)
	assert.Equal(t, 1, result.Applied)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, int64(1), result.Pending)

	var rows []payableEarningIntentRow
	require.NoError(t, app.DB.Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, payableEarningStatusPending, rows[0].Status)
	assert.Equal(t, payableEarningStatusApplied, rows[1].Status)
	wallet, err := app.AgentRepo.GetWallet(ctx, healthy.TenantID)
	require.NoError(t, err)
	assert.Equal(t, healthy.Amount, wallet.WithdrawableBalance)
}
