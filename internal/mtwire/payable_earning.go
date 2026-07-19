package mtwire

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/internal/agent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	payableEarningStatusPending = "pending"
	payableEarningStatusApplied = "applied"
)

// payableEarningIntentRow is the durable hand-off between request billing and
// the idempotent agent earning sink. A pending row is safe to replay after a
// process restart, including when the earning committed but the status update
// did not.
type payableEarningIntentRow struct {
	ID               int64      `gorm:"column:id;primaryKey;autoIncrement"`
	IdemKey          string     `gorm:"column:idem_key;type:varchar(200);not null;uniqueIndex:idx_mt_payable_earning_idem"`
	TenantID         int64      `gorm:"column:tenant_id;not null;index:idx_mt_payable_earning_tenant"`
	UserID           int64      `gorm:"column:user_id;not null;default:0"`
	SourceType       string     `gorm:"column:source_type;type:varchar(32);not null"`
	SourceID         string     `gorm:"column:source_id;type:varchar(128);not null"`
	Amount           float64    `gorm:"column:amount;type:decimal(20,8);not null"`
	Remark           string     `gorm:"column:remark;type:varchar(255);not null;default:''"`
	EarningCreatedAt *time.Time `gorm:"column:earning_created_at"`
	Status           string     `gorm:"column:status;type:varchar(16);not null;index:idx_mt_payable_earning_status"`
	Attempts         int        `gorm:"column:attempts;not null;default:0"`
	LastError        string     `gorm:"column:last_error;type:varchar(512);not null;default:''"`
	CreatedAt        time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;not null;index:idx_mt_payable_earning_updated"`
	AppliedAt        *time.Time `gorm:"column:applied_at"`
}

func (payableEarningIntentRow) TableName() string { return "mt_payable_earning_intents" }

func migratePayableEarningIntents(db *gorm.DB) error {
	return db.AutoMigrate(&payableEarningIntentRow{})
}

func normalizePayableEarning(entry agent.EarningEntry) agent.EarningEntry {
	entry.Amount = math.Round(entry.Amount*1e8) / 1e8
	if !entry.CreatedAt.IsZero() {
		entry.CreatedAt = entry.CreatedAt.UTC().Truncate(time.Millisecond)
	}
	return entry
}

func payableEarningMatches(row *payableEarningIntentRow, entry agent.EarningEntry) bool {
	if row.TenantID != entry.TenantID || row.UserID != entry.UserID ||
		row.SourceType != string(entry.SourceType) || row.SourceID != entry.SourceID ||
		row.Amount != entry.Amount || row.Remark != entry.Remark {
		return false
	}
	if row.EarningCreatedAt == nil {
		return entry.CreatedAt.IsZero()
	}
	return !entry.CreatedAt.IsZero() && row.EarningCreatedAt.UTC().Truncate(time.Millisecond).Equal(entry.CreatedAt)
}

func (a *App) ensurePayableEarningIntent(ctx context.Context, entry agent.EarningEntry) (*payableEarningIntentRow, error) {
	if a == nil || a.DB == nil {
		return nil, errors.New("payable earning database is unavailable")
	}
	entry = normalizePayableEarning(entry)
	if err := entry.Validate(); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	row := &payableEarningIntentRow{
		IdemKey:    entry.IdempotencyKey(),
		TenantID:   entry.TenantID,
		UserID:     entry.UserID,
		SourceType: string(entry.SourceType),
		SourceID:   entry.SourceID,
		Amount:     entry.Amount,
		Remark:     entry.Remark,
		Status:     payableEarningStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if !entry.CreatedAt.IsZero() {
		createdAt := entry.CreatedAt
		row.EarningCreatedAt = &createdAt
	}
	result := a.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "idem_key"}},
		DoNothing: true,
	}).Create(row)
	if result.Error != nil {
		return nil, result.Error
	}
	if err := a.DB.WithContext(ctx).Where("idem_key = ?", row.IdemKey).First(row).Error; err != nil {
		return nil, err
	}
	if !payableEarningMatches(row, entry) {
		return nil, errors.New("payable earning retry does not match persisted payload")
	}
	return row, nil
}

func (row *payableEarningIntentRow) earningEntry() agent.EarningEntry {
	entry := agent.EarningEntry{
		TenantID:   row.TenantID,
		UserID:     row.UserID,
		SourceType: agent.EarningSource(row.SourceType),
		SourceID:   row.SourceID,
		Amount:     row.Amount,
		Remark:     row.Remark,
	}
	if row.EarningCreatedAt != nil {
		entry.CreatedAt = *row.EarningCreatedAt
	}
	return entry
}

func truncatePayableEarningError(message string) string {
	const maxLength = 512
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength]
}

func (a *App) applyPayableEarningIntent(ctx context.Context, idemKey string) error {
	if a == nil || a.DB == nil {
		return errors.New("payable earning database is unavailable")
	}
	if a.AgentEarnings == nil {
		return errors.New("payable earning sink is unavailable")
	}
	var row payableEarningIntentRow
	if err := a.DB.WithContext(ctx).Where("idem_key = ?", idemKey).First(&row).Error; err != nil {
		return err
	}
	if row.Status == payableEarningStatusApplied {
		return nil
	}
	if row.Status != payableEarningStatusPending {
		return fmt.Errorf("payable earning has invalid status %q", row.Status)
	}

	err := a.AgentEarnings.AddEarning(ctx, row.earningEntry())
	if err != nil {
		_ = a.DB.WithContext(ctx).Model(&payableEarningIntentRow{}).
			Where("id = ? AND status = ?", row.ID, payableEarningStatusPending).
			Updates(map[string]interface{}{
				"attempts":   gorm.Expr("attempts + 1"),
				"last_error": truncatePayableEarningError(err.Error()),
				"updated_at": time.Now(),
			}).Error
		return err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	result := a.DB.WithContext(ctx).Model(&payableEarningIntentRow{}).
		Where("id = ? AND status = ?", row.ID, payableEarningStatusPending).
		Updates(map[string]interface{}{
			"status":     payableEarningStatusApplied,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": "",
			"updated_at": now,
			"applied_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current payableEarningIntentRow
		if err := a.DB.WithContext(ctx).Where("id = ?", row.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Status != payableEarningStatusApplied {
			return errors.New("payable earning state changed concurrently")
		}
	}
	return nil
}

type payableEarningReconcileResult struct {
	Scanned int
	Applied int
	Failed  int
	Pending int64
}

func (a *App) reconcilePayableEarningIntents(ctx context.Context, limit int) (payableEarningReconcileResult, error) {
	result := payableEarningReconcileResult{}
	if a == nil || a.DB == nil || a.AgentEarnings == nil {
		return result, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var rows []payableEarningIntentRow
	if err := a.DB.WithContext(ctx).Where("status = ?", payableEarningStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return result, err
	}
	result.Scanned = len(rows)
	var firstErr error
	for _, row := range rows {
		if err := a.applyPayableEarningIntent(ctx, row.IdemKey); err != nil {
			result.Failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Applied++
	}
	if err := a.DB.WithContext(ctx).Model(&payableEarningIntentRow{}).
		Where("status = ?", payableEarningStatusPending).Count(&result.Pending).Error; err != nil && firstErr == nil {
		firstErr = err
	}
	return result, firstErr
}
