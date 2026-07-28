package gormrepo

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
)

// The compatibility triggers are deliberately permanent. A previous release
// knows only the decimal columns, while the current release treats *_units as
// authoritative. Keeping both directions synchronized makes a rolling deploy
// and a release-only rollback safe without teaching the old binary about the
// expanded schema.
const moneyTriggerVersion = "v2"

// SQLite stores the legacy decimal columns as binary64 REAL values. Very close
// to the int64/1e8 boundary, distinct in-range and out-of-range decimals become
// the same float and CAST silently saturates. Keep a material safety margin for
// decimal-authoritative legacy writes. Unit-authoritative current writes retain
// the full int64 range; only an old binary attempting to rewrite such an extreme
// balance is rejected rather than rounded ambiguously.
const sqliteLegacyMoneyLimit = "90000000000.00000000"

type agentMoneyColumn struct {
	amount string
	units  string
}

type agentMoneyTable struct {
	name    string
	key     string
	columns []agentMoneyColumn
}

var agentMoneyTables = []agentMoneyTable{
	{
		name: "agent_wallets",
		key:  "tenant_id",
		columns: []agentMoneyColumn{
			{amount: "api_balance", units: "api_balance_units"},
			{amount: "withdrawable_balance", units: "withdrawable_balance_units"},
			{amount: "frozen_withdraw_amount", units: "frozen_withdraw_amount_units"},
			{amount: "total_earned", units: "total_earned_units"},
		},
	},
	{
		name:    "agent_earning_logs",
		key:     "id",
		columns: []agentMoneyColumn{{amount: "amount", units: "amount_units"}},
	},
	{
		name:    "agent_withdrawals",
		key:     "id",
		columns: []agentMoneyColumn{{amount: "amount", units: "amount_units"}},
	},
}

func installAgentMoneyCompatibilityTriggers(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite":
		return installSQLiteMoneyTriggers(db)
	case "mysql":
		return installMySQLMoneyTriggers(db)
	case "postgres":
		return installPostgresMoneyTriggers(db)
	default:
		return fmt.Errorf("agent money migration: unsupported database dialect %q", db.Dialector.Name())
	}
}

// backfillAgentMoneyUnits is a one-time import of the legacy decimal values.
// The claim token, rather than RowsAffected, identifies the transaction that
// owns the migration. This remains correct with MySQL clientFoundRows=true.
// Existing markers return immediately: startup never rewrites the history
// tables just to repair a read-model mirror.
func backfillAgentMoneyUnits(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		claimToken := common.GetUUID()
		marker := agentSchemaMigrationRow{
			Key:         moneyUnitsMigrationKey,
			ClaimToken:  claimToken,
			CompletedAt: time.Now(),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return fmt.Errorf("claim agent money migration: %w", err)
		}
		var persisted agentSchemaMigrationRow
		if err := tx.Where(&agentSchemaMigrationRow{Key: moneyUnitsMigrationKey}).Take(&persisted).Error; err != nil {
			return fmt.Errorf("read agent money migration claim: %w", err)
		}
		if persisted.ClaimToken != claimToken {
			return nil
		}

		maxAmount := decimal.NewFromInt(maxMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
		minAmount := decimal.NewFromInt(minMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
		if tx.Dialector.Name() == "sqlite" {
			maxAmount = sqliteLegacyMoneyLimit
			minAmount = "-" + sqliteLegacyMoneyLimit
		}

		for _, table := range agentMoneyTables {
			for _, column := range table.columns {
				var outOfRange int64
				rangeQuery := fmt.Sprintf("%s > ? OR %s < ?", column.amount, column.amount)
				if err := tx.Table(table.name).Where(rangeQuery, maxAmount, minAmount).Count(&outOfRange).Error; err != nil {
					return fmt.Errorf("validate %s.%s fixed-point range: %w", table.name, column.amount, err)
				}
				if outOfRange != 0 {
					return fmt.Errorf("%s.%s has %d values outside safe fixed-point migration range [%s,%s]", table.name, column.amount, outOfRange, minAmount, maxAmount)
				}
			}
		}

		for _, table := range agentMoneyTables {
			for _, column := range table.columns {
				unitsSQL, err := moneyBackfillUnitExpression(tx.Dialector.Name(), column.amount)
				if err != nil {
					return err
				}
				query := fmt.Sprintf("UPDATE %s SET %s = %s", table.name, column.units, unitsSQL)
				if err := tx.Exec(query).Error; err != nil {
					return fmt.Errorf("backfill %s.%s: %w", table.name, column.units, err)
				}
			}
		}

		for _, table := range agentMoneyTables {
			for _, column := range table.columns {
				var nullRows int64
				if err := tx.Table(table.name).Where(column.units + " IS NULL").Count(&nullRows).Error; err != nil {
					return fmt.Errorf("validate %s.%s nulls: %w", table.name, column.units, err)
				}
				if nullRows != 0 {
					return fmt.Errorf("%s.%s has %d NULL values after money migration", table.name, column.units, nullRows)
				}
			}
		}
		return nil
	})
}

func moneyMirrorSQLExpression(dialect, unitColumn string) (string, error) {
	switch dialect {
	case "sqlite":
		return fmt.Sprintf("%s / %d.0", unitColumn, moneyScaleUnits), nil
	case "mysql", "postgres":
		// BIGINT has up to 19 integer digits. DECIMAL(28,8) preserves all of
		// them and gives MySQL division enough input scale to retain 1e-8.
		return fmt.Sprintf("CAST(%s AS DECIMAL(28,8)) / %d", unitColumn, moneyScaleUnits), nil
	default:
		return "", fmt.Errorf("agent money migration: unsupported database dialect %q", dialect)
	}
}

func moneyBackfillUnitExpression(dialect, column string) (string, error) {
	switch dialect {
	case "sqlite":
		return fmt.Sprintf("CAST(ROUND(%s * %d.0, 0) AS INTEGER)", column, moneyScaleUnits), nil
	case "mysql":
		return fmt.Sprintf("CAST(ROUND(%s * %d, 0) AS SIGNED)", column, moneyScaleUnits), nil
	case "postgres":
		return fmt.Sprintf("CAST(ROUND(%s * %d, 0) AS BIGINT)", column, moneyScaleUnits), nil
	default:
		return "", fmt.Errorf("agent money migration: unsupported database dialect %q", dialect)
	}
}

func installMySQLMoneyTriggers(db *gorm.DB) error {
	maxAmount := decimal.NewFromInt(maxMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
	minAmount := decimal.NewFromInt(minMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
	for _, table := range agentMoneyTables {
		insertName := fmt.Sprintf("trg_%s_money_%s_bi", table.name, moneyTriggerVersion)
		var insertBody strings.Builder
		insertBody.WriteString("BEGIN\n")
		for _, column := range table.columns {
			fmt.Fprintf(&insertBody, "IF NEW.%s IS NULL OR (NEW.%s = 0 AND ROUND(NEW.%s * %d, 0) <> 0) THEN\n", column.units, column.units, column.amount, moneyScaleUnits)
			fmt.Fprintf(&insertBody, "IF NEW.%s > %s OR NEW.%s < %s THEN SIGNAL SQLSTATE '22003' SET MESSAGE_TEXT = 'agent money amount out of range'; END IF;\n", column.amount, maxAmount, column.amount, minAmount)
			fmt.Fprintf(&insertBody, "SET NEW.%s = CAST(ROUND(NEW.%s * %d, 0) AS SIGNED);\n", column.units, column.amount, moneyScaleUnits)
			insertBody.WriteString("END IF;\n")
			mirror, _ := moneyMirrorSQLExpression("mysql", "NEW."+column.units)
			fmt.Fprintf(&insertBody, "SET NEW.%s = %s;\n", column.amount, mirror)
		}
		insertBody.WriteString("END")
		if err := createMySQLTriggerIfMissing(db, insertName, fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW %s", insertName, table.name, insertBody.String(),
		)); err != nil {
			return err
		}

		updateName := fmt.Sprintf("trg_%s_money_%s_bu", table.name, moneyTriggerVersion)
		var updateBody strings.Builder
		updateBody.WriteString("BEGIN\n")
		for _, column := range table.columns {
			fmt.Fprintf(&updateBody, "IF NEW.%s IS NULL OR ((NEW.%s <=> OLD.%s) AND (NOT (NEW.%s <=> OLD.%s) OR (NEW.%s = 0 AND ROUND(NEW.%s * %d, 0) <> 0))) THEN\n", column.units, column.units, column.units, column.amount, column.amount, column.units, column.amount, moneyScaleUnits)
			fmt.Fprintf(&updateBody, "IF NEW.%s > %s OR NEW.%s < %s THEN SIGNAL SQLSTATE '22003' SET MESSAGE_TEXT = 'agent money amount out of range'; END IF;\n", column.amount, maxAmount, column.amount, minAmount)
			fmt.Fprintf(&updateBody, "SET NEW.%s = CAST(ROUND(NEW.%s * %d, 0) AS SIGNED);\n", column.units, column.amount, moneyScaleUnits)
			updateBody.WriteString("END IF;\n")
			mirror, _ := moneyMirrorSQLExpression("mysql", "NEW."+column.units)
			fmt.Fprintf(&updateBody, "SET NEW.%s = %s;\n", column.amount, mirror)
		}
		updateBody.WriteString("END")
		if err := createMySQLTriggerIfMissing(db, updateName, fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE UPDATE ON %s FOR EACH ROW %s", updateName, table.name, updateBody.String(),
		)); err != nil {
			return err
		}
	}
	return nil
}

func createMySQLTriggerIfMissing(db *gorm.DB, name, statement string) error {
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE() AND TRIGGER_NAME = ?`, name).Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect MySQL trigger %s: %w", name, err)
	}
	if count != 0 {
		return nil
	}
	if err := execMoneyDDL(db, statement); err != nil {
		if checkErr := db.Raw(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE() AND TRIGGER_NAME = ?`, name).Scan(&count).Error; checkErr == nil && count != 0 {
			return nil
		}
		return fmt.Errorf("create MySQL trigger %s: %w", name, err)
	}
	return nil
}

func installPostgresMoneyTriggers(db *gorm.DB) error {
	maxAmount := decimal.NewFromInt(maxMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
	minAmount := decimal.NewFromInt(minMoneyUnits).Shift(-moneyScale).StringFixed(moneyScale)
	for _, table := range agentMoneyTables {
		functionName := fmt.Sprintf("%s_money_%s_sync", table.name, moneyTriggerVersion)
		var body strings.Builder
		body.WriteString("BEGIN\nIF TG_OP = 'INSERT' THEN\n")
		for _, column := range table.columns {
			fmt.Fprintf(&body, "IF NEW.%s IS NULL OR (NEW.%s = 0 AND ROUND(NEW.%s * %d, 0) <> 0) THEN\n", column.units, column.units, column.amount, moneyScaleUnits)
			fmt.Fprintf(&body, "IF NEW.%s > %s OR NEW.%s < %s THEN RAISE EXCEPTION 'agent money amount out of range' USING ERRCODE = '22003'; END IF;\n", column.amount, maxAmount, column.amount, minAmount)
			fmt.Fprintf(&body, "NEW.%s := CAST(ROUND(NEW.%s * %d, 0) AS BIGINT);\n", column.units, column.amount, moneyScaleUnits)
			body.WriteString("END IF;\n")
			mirror, _ := moneyMirrorSQLExpression("postgres", "NEW."+column.units)
			fmt.Fprintf(&body, "NEW.%s := %s;\n", column.amount, mirror)
		}
		body.WriteString("ELSE\n")
		for _, column := range table.columns {
			fmt.Fprintf(&body, "IF NEW.%s IS NULL OR ((NEW.%s IS NOT DISTINCT FROM OLD.%s) AND (NEW.%s IS DISTINCT FROM OLD.%s OR (NEW.%s = 0 AND ROUND(NEW.%s * %d, 0) <> 0))) THEN\n", column.units, column.units, column.units, column.amount, column.amount, column.units, column.amount, moneyScaleUnits)
			fmt.Fprintf(&body, "IF NEW.%s > %s OR NEW.%s < %s THEN RAISE EXCEPTION 'agent money amount out of range' USING ERRCODE = '22003'; END IF;\n", column.amount, maxAmount, column.amount, minAmount)
			fmt.Fprintf(&body, "NEW.%s := CAST(ROUND(NEW.%s * %d, 0) AS BIGINT);\n", column.units, column.amount, moneyScaleUnits)
			body.WriteString("END IF;\n")
			mirror, _ := moneyMirrorSQLExpression("postgres", "NEW."+column.units)
			fmt.Fprintf(&body, "NEW.%s := %s;\n", column.amount, mirror)
		}
		body.WriteString("END IF;\nRETURN NEW;\nEND;")

		functionSQL := fmt.Sprintf(
			"CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $agent_money$\n%s\n$agent_money$", functionName, body.String(),
		)
		if err := createPostgresFunctionIfMissing(db, functionName, functionSQL); err != nil {
			return err
		}
		triggerName := fmt.Sprintf("trg_%s_money_%s_biu", table.name, moneyTriggerVersion)
		if err := createPostgresTriggerIfMissing(db, triggerName, fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE PROCEDURE %s()", triggerName, table.name, functionName,
		)); err != nil {
			return err
		}
	}
	return nil
}

func createPostgresFunctionIfMissing(db *gorm.DB, name, statement string) error {
	var count int64
	query := `SELECT COUNT(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = current_schema() AND p.proname = ?`
	if err := db.Raw(query, name).Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect PostgreSQL function %s: %w", name, err)
	}
	if count != 0 {
		return nil
	}
	if err := execMoneyDDL(db, statement); err != nil {
		if checkErr := db.Raw(query, name).Scan(&count).Error; checkErr == nil && count != 0 {
			return nil
		}
		return fmt.Errorf("create PostgreSQL function %s: %w", name, err)
	}
	return nil
}

func createPostgresTriggerIfMissing(db *gorm.DB, name, statement string) error {
	var count int64
	query := `SELECT COUNT(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND t.tgname = ? AND NOT t.tgisinternal`
	if err := db.Raw(query, name).Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect PostgreSQL trigger %s: %w", name, err)
	}
	if count != 0 {
		return nil
	}
	if err := execMoneyDDL(db, statement); err != nil {
		if checkErr := db.Raw(query, name).Scan(&count).Error; checkErr == nil && count != 0 {
			return nil
		}
		return fmt.Errorf("create PostgreSQL trigger %s: %w", name, err)
	}
	return nil
}

func installSQLiteMoneyTriggers(db *gorm.DB) error {
	for _, table := range agentMoneyTables {
		insertValidate := fmt.Sprintf("trg_%s_money_%s_bi_validate", table.name, moneyTriggerVersion)
		var validation strings.Builder
		validation.WriteString("BEGIN\n")
		for _, column := range table.columns {
			fmt.Fprintf(&validation, "SELECT CASE WHEN NEW.%s IS NOT NULL AND typeof(NEW.%s) <> 'integer' THEN RAISE(ABORT, 'agent money units must be integer') END;\n", column.units, column.units)
			fmt.Fprintf(&validation, "SELECT CASE WHEN (NEW.%s IS NULL OR (NEW.%s = 0 AND ROUND(NEW.%s * %d.0, 0) <> 0)) AND (NEW.%s > %s OR NEW.%s < -%s) THEN RAISE(ABORT, 'agent money amount outside safe SQLite legacy range') END;\n", column.units, column.units, column.amount, moneyScaleUnits, column.amount, sqliteLegacyMoneyLimit, column.amount, sqliteLegacyMoneyLimit)
		}
		validation.WriteString("END")
		if err := createSQLiteTriggerIfMissing(db, insertValidate, fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW %s", insertValidate, table.name, validation.String(),
		)); err != nil {
			return err
		}

		insertSync := fmt.Sprintf("trg_%s_money_%s_ai_sync", table.name, moneyTriggerVersion)
		insertMismatch := sqlitePairMismatch("NEW", table.columns)
		insertAssignments := sqliteInsertAssignments(table.columns)
		if err := createSQLiteTriggerIfMissing(db, insertSync, fmt.Sprintf(
			"CREATE TRIGGER %s AFTER INSERT ON %s FOR EACH ROW WHEN %s BEGIN UPDATE %s SET %s WHERE %s = NEW.%s; END",
			insertSync, table.name, insertMismatch, table.name, insertAssignments, table.key, table.key,
		)); err != nil {
			return err
		}

		updateValidate := fmt.Sprintf("trg_%s_money_%s_bu_validate", table.name, moneyTriggerVersion)
		validation.Reset()
		validation.WriteString("BEGIN\n")
		for _, column := range table.columns {
			fmt.Fprintf(&validation, "SELECT CASE WHEN NEW.%s IS NOT NULL AND typeof(NEW.%s) <> 'integer' THEN RAISE(ABORT, 'agent money units must be integer') END;\n", column.units, column.units)
			legacySource := fmt.Sprintf("NEW.%s IS NULL OR ((NEW.%s IS OLD.%s) AND ((NEW.%s IS NOT OLD.%s) OR (NEW.%s = 0 AND ROUND(NEW.%s * %d.0, 0) <> 0)))", column.units, column.units, column.units, column.amount, column.amount, column.units, column.amount, moneyScaleUnits)
			fmt.Fprintf(&validation, "SELECT CASE WHEN (%s) AND (NEW.%s > %s OR NEW.%s < -%s) THEN RAISE(ABORT, 'agent money amount outside safe SQLite legacy range') END;\n", legacySource, column.amount, sqliteLegacyMoneyLimit, column.amount, sqliteLegacyMoneyLimit)
		}
		validation.WriteString("END")
		if err := createSQLiteTriggerIfMissing(db, updateValidate, fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE UPDATE ON %s FOR EACH ROW %s", updateValidate, table.name, validation.String(),
		)); err != nil {
			return err
		}

		updateSync := fmt.Sprintf("trg_%s_money_%s_au_sync", table.name, moneyTriggerVersion)
		updateAssignments := sqliteUpdateAssignments(table.columns)
		if err := createSQLiteTriggerIfMissing(db, updateSync, fmt.Sprintf(
			"CREATE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW WHEN %s BEGIN UPDATE %s SET %s WHERE %s = NEW.%s; END",
			updateSync, table.name, insertMismatch, table.name, updateAssignments, table.key, table.key,
		)); err != nil {
			return err
		}
	}
	return nil
}

func sqlitePairMismatch(row string, columns []agentMoneyColumn) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, fmt.Sprintf("%s.%s IS NULL OR %s.%s IS NOT (%s.%s / %d.0)", row, column.units, row, column.amount, row, column.units, moneyScaleUnits))
	}
	return strings.Join(parts, " OR ")
}

func sqliteInsertAssignments(columns []agentMoneyColumn) string {
	assignments := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		targetUnits := fmt.Sprintf("CASE WHEN NEW.%s IS NULL OR (NEW.%s = 0 AND ROUND(NEW.%s * %d.0, 0) <> 0) THEN CAST(ROUND(NEW.%s * %d.0, 0) AS INTEGER) ELSE NEW.%s END", column.units, column.units, column.amount, moneyScaleUnits, column.amount, moneyScaleUnits, column.units)
		assignments = append(assignments,
			fmt.Sprintf("%s = %s", column.units, targetUnits),
			fmt.Sprintf("%s = (%s) / %d.0", column.amount, targetUnits, moneyScaleUnits),
		)
	}
	return strings.Join(assignments, ", ")
}

func sqliteUpdateAssignments(columns []agentMoneyColumn) string {
	assignments := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		targetUnits := fmt.Sprintf("CASE WHEN NEW.%s IS NULL THEN CAST(ROUND(NEW.%s * %d.0, 0) AS INTEGER) WHEN NEW.%s IS NOT OLD.%s THEN NEW.%s WHEN NEW.%s IS NOT OLD.%s THEN CAST(ROUND(NEW.%s * %d.0, 0) AS INTEGER) WHEN NEW.%s = 0 AND ROUND(NEW.%s * %d.0, 0) <> 0 THEN CAST(ROUND(NEW.%s * %d.0, 0) AS INTEGER) ELSE NEW.%s END", column.units, column.amount, moneyScaleUnits, column.units, column.units, column.units, column.amount, column.amount, column.amount, moneyScaleUnits, column.units, column.amount, moneyScaleUnits, column.amount, moneyScaleUnits, column.units)
		assignments = append(assignments,
			fmt.Sprintf("%s = %s", column.units, targetUnits),
			fmt.Sprintf("%s = (%s) / %d.0", column.amount, targetUnits, moneyScaleUnits),
		)
	}
	return strings.Join(assignments, ", ")
}

func createSQLiteTriggerIfMissing(db *gorm.DB, name, statement string) error {
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect SQLite trigger %s: %w", name, err)
	}
	if count != 0 {
		return nil
	}
	if err := execMoneyDDL(db, statement); err != nil {
		if checkErr := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&count).Error; checkErr == nil && count != 0 {
			return nil
		}
		return fmt.Errorf("create SQLite trigger %s: %w", name, err)
	}
	return nil
}

// Production enables GORM's global prepared-statement cache. MySQL 5.7 does
// not accept CREATE TRIGGER through every prepared-statement path, so schema
// DDL deliberately unwraps that cache and uses the driver's direct Exec path.
func execMoneyDDL(db *gorm.DB, statement string) error {
	conn := db.Statement.ConnPool
	if prepared, ok := conn.(*gorm.PreparedStmtDB); ok {
		conn = prepared.ConnPool
	}
	_, err := conn.ExecContext(db.Statement.Context, statement)
	return err
}
