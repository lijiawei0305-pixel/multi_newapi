package reportrepo

import (
	"fmt"
)

const reportMoneyScaleUnits int64 = 100_000_000

type reportAgentEarningMoneySchema struct {
	AmountUnits *int64 `gorm:"column:amount_units"`
}

func (reportAgentEarningMoneySchema) TableName() string { return "agent_earning_logs" }

type reportAgentWithdrawalMoneySchema struct {
	AmountUnits *int64 `gorm:"column:amount_units"`
}

func (reportAgentWithdrawalMoneySchema) TableName() string { return "agent_withdrawals" }

type reportAgentWalletMoneySchema struct {
	APIBalanceUnits           *int64 `gorm:"column:api_balance_units"`
	WithdrawableBalanceUnits  *int64 `gorm:"column:withdrawable_balance_units"`
	FrozenWithdrawAmountUnits *int64 `gorm:"column:frozen_withdraw_amount_units"`
	TotalEarnedUnits          *int64 `gorm:"column:total_earned_units"`
}

func (reportAgentWalletMoneySchema) TableName() string { return "agent_wallets" }

type reportAgentMigrationSchema struct {
	Key string `gorm:"column:key"`
}

func (reportAgentMigrationSchema) TableName() string { return "agent_schema_migrations" }

// agentMoneyUnitColumn selects the authoritative fixed-point column when the
// agent money migration has run. The legacy decimal fallback keeps reportrepo
// usable in isolated fixtures and during the expand side of a rolling deploy;
// production reaches this repository only after agent AutoMigrate completes.
func (r *Repo) agentMoneyUnitColumn(table, legacyColumn string) (string, bool, error) {
	valid := false
	var schema any
	switch table {
	case "agent_earning_logs", "agent_withdrawals":
		valid = legacyColumn == "amount"
		if table == "agent_earning_logs" {
			schema = &reportAgentEarningMoneySchema{}
		} else {
			schema = &reportAgentWithdrawalMoneySchema{}
		}
	case "agent_wallets":
		schema = &reportAgentWalletMoneySchema{}
		switch legacyColumn {
		case "api_balance", "withdrawable_balance", "frozen_withdraw_amount", "total_earned":
			valid = true
		}
	}
	if !valid {
		return legacyColumn + "_units", false, nil
	}

	key := table + "." + legacyColumn
	if cached, ok := r.moneyUnitColumns.Load(key); ok {
		return legacyColumn + "_units", cached.(bool), nil
	}
	hasUnits := r.db.Migrator().HasColumn(schema, legacyColumn+"_units")
	if !hasUnits {
		// Repo may be constructed before App.Migrate. Do not permanently cache
		// an expand-phase miss; the first post-migration request must switch to
		// authoritative units without requiring a process restart.
		return legacyColumn + "_units", false, nil
	}
	if !r.db.Migrator().HasTable(&reportAgentMigrationSchema{}) {
		return legacyColumn + "_units", false, nil
	}
	var completed int64
	if err := r.db.Table("agent_schema_migrations").
		Where(map[string]interface{}{"key": "money_units_v1"}).
		Count(&completed).Error; err != nil {
		return "", false, fmt.Errorf("read agent money migration marker: %w", err)
	}
	if completed != 1 {
		// AutoMigrate expands the nullable columns before its transactional
		// backfill commits. Other nodes must keep reading the complete legacy
		// mirrors during that window; SUM(nullable_units) would otherwise turn
		// an in-progress or rolled-back migration into a believable zero.
		return legacyColumn + "_units", false, nil
	}
	actual, _ := r.moneyUnitColumns.LoadOrStore(key, true)
	return legacyColumn + "_units", actual.(bool), nil
}

func reportMoneyFromUnits(units int64) float64 {
	return float64(units) / float64(reportMoneyScaleUnits)
}

func addReportMoneyUnits(total, delta int64) (int64, error) {
	const maxInt64 = int64(^uint64(0) >> 1)
	const minInt64 = -maxInt64 - 1
	if delta > 0 && total > maxInt64-delta {
		return 0, fmt.Errorf("agent report money units overflow")
	}
	if delta < 0 && total < minInt64-delta {
		return 0, fmt.Errorf("agent report money units overflow")
	}
	return total + delta, nil
}

func validateReportMoneyUnitCount(table, column string, rowCount, unitCount int64) error {
	if rowCount == unitCount {
		return nil
	}
	return fmt.Errorf("%s.%s has %d NULL authoritative money values", table, column, rowCount-unitCount)
}
