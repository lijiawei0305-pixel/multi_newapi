// Package gormrepo 用真实 GORM(MySQL) 实现 agent.AgentRepo（代理资料 / 钱包 / 收益台账 / 提现单 四表）。
//
// 键统一为 tenant_id（决策：代理=User+Tenant 1:1）。关键不变量（detailed-design §6.2）：
//   - AppendEarning：收益日志 idem_key 唯一索引 + ON CONFLICT DO NOTHING 强幂等；仅首次入账才动钱包，
//     钱包用 upsert 原子累加（withdrawable += amount、total_earned += amount）。
//   - CreateWithdrawal：条件 UPDATE（WHERE withdrawable >= amount）在 DB 层保证「不透支」，
//     0 行受影响即余额不足 -> ErrWithdrawInsufficient；同事务再建 pending 提现单（金额守恒：可提现→冻结）。
//   - ResolveWithdrawal：CAS（WHERE status='pending'）原子翻牌，杜绝并发重复审核；approved 不动钱，
//     rejected=解冻退回（金额守恒：冻结→可提现）；MarkWithdrawalPaid 才扣冻结出账。
//
// 金额以 BIGINT 的 1e-8 单位列为账务权威值；既有 decimal(20,8) 列保留为兼容镜像，接口仍以 float64 进出。
package gormrepo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
)

// ---- 表 1：agent_profiles —— 代理资料（tenant_id 主键，1:1 独占） ----

type profileRow struct {
	TenantID         int64   `gorm:"column:tenant_id;primaryKey"`
	UserID           int64   `gorm:"column:user_id;not null;default:0;index:idx_agent_profiles_user"`
	Level            int     `gorm:"column:level;not null;default:0"`
	CanAPI           bool    `gorm:"column:can_api;not null;default:false"`
	CostPriceCNY     float64 `gorm:"column:cost_price_cny;type:decimal(20,8);not null;default:0"`
	PackageDiscount  float64 `gorm:"column:package_discount;type:decimal(20,8);not null;default:0"`
	CommissionRatio  float64 `gorm:"column:commission_ratio;type:decimal(20,8);not null;default:0"`
	DiscountFloor    float64 `gorm:"column:discount_floor;type:decimal(20,8);not null;default:0"`
	BottomPriceRatio float64 `gorm:"column:bottom_price_ratio;type:decimal(20,8);not null;default:0"`
	DiscountRatio    float64 `gorm:"column:discount_ratio;type:decimal(20,8);not null;default:0"`
	// PayoutMethod/PayoutAccount/PayoutName/PayoutBank：代理收款账户（提现闭环补强 #1）。
	// 代理自助设置/修改（GetPayoutAccount/SetPayoutAccount，不经 AgentParams/SetAgentType）；
	// 申请提现时整份快照进 agent_withdrawals（见 withdrawalRow 同名字段）。
	PayoutMethod  string    `gorm:"column:payout_method;type:varchar(16);not null;default:''"`
	PayoutAccount string    `gorm:"column:payout_account;type:varchar(128);not null;default:''"`
	PayoutName    string    `gorm:"column:payout_name;type:varchar(64);not null;default:''"`
	PayoutBank    string    `gorm:"column:payout_bank;type:varchar(128);not null;default:''"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (profileRow) TableName() string { return "agent_profiles" }

// ---- 表 2：agent_wallets —— 代理钱包（tenant_id 主键） ----

type walletRow struct {
	TenantID                  int64     `gorm:"column:tenant_id;primaryKey"`
	UserID                    int64     `gorm:"column:user_id;not null;default:0"`
	APIBalance                float64   `gorm:"column:api_balance;type:decimal(20,8);not null;default:0"`
	APIBalanceUnits           int64     `gorm:"column:api_balance_units;type:bigint"`
	WithdrawableBalance       float64   `gorm:"column:withdrawable_balance;type:decimal(20,8);not null;default:0"`
	WithdrawableBalanceUnits  int64     `gorm:"column:withdrawable_balance_units;type:bigint"`
	FrozenWithdrawAmount      float64   `gorm:"column:frozen_withdraw_amount;type:decimal(20,8);not null;default:0"`
	FrozenWithdrawAmountUnits int64     `gorm:"column:frozen_withdraw_amount_units;type:bigint"`
	TotalEarned               float64   `gorm:"column:total_earned;type:decimal(20,8);not null;default:0"`
	TotalEarnedUnits          int64     `gorm:"column:total_earned_units;type:bigint"`
	UpdatedAt                 time.Time `gorm:"column:updated_at"`
}

func (walletRow) TableName() string { return "agent_wallets" }

// ---- 表 3：agent_earning_logs —— 收益台账（idem_key 唯一，强幂等） ----

type earningRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    int64     `gorm:"column:tenant_id;not null;index:idx_agent_earnings_tenant;index:idx_agent_earnings_source_lookup,priority:1"`
	UserID      int64     `gorm:"column:user_id;not null;default:0"`
	SourceType  string    `gorm:"column:source_type;type:varchar(32);not null;index:idx_agent_earnings_source_lookup,priority:2"`
	SourceID    string    `gorm:"column:source_id;type:varchar(128);not null;index:idx_agent_earnings_source_lookup,priority:3"`
	IdemKey     string    `gorm:"column:idem_key;type:varchar(200);not null"`
	IdemKeyHash *string   `gorm:"column:idem_key_hash;type:varchar(64);uniqueIndex:idx_agent_earnings_idem_hash"`
	ClaimID     string    `gorm:"column:claim_id;type:varchar(64);not null;default:''"`
	Amount      float64   `gorm:"column:amount;type:decimal(20,8);not null"`
	AmountUnits int64     `gorm:"column:amount_units;type:bigint"`
	Remark      string    `gorm:"column:remark;type:varchar(255);not null;default:''"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (earningRow) TableName() string { return "agent_earning_logs" }

const (
	moneyScale      int32 = 8
	moneyScaleUnits int64 = 100_000_000
	moneyCentUnits  int64 = 1_000_000
	maxMoneyUnits         = int64(^uint64(0) >> 1)
	minMoneyUnits         = -maxMoneyUnits - 1
)

var errWalletInvariant = agent.ErrWalletInvariant

// moneyUnits converts the public float64 boundary into the repository's
// authoritative fixed-point representation. shopspring/decimal deliberately
// performs the rounding before the range check, so every persisted amount has
// exactly eight decimal places without float multiplication overflow/dust.
func moneyUnits(amount float64) (int64, bool) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, false
	}
	scaled := decimal.NewFromFloat(amount).Round(moneyScale).Shift(moneyScale)
	if scaled.GreaterThan(decimal.NewFromInt(maxMoneyUnits)) || scaled.LessThan(decimal.NewFromInt(minMoneyUnits)) {
		return 0, false
	}
	return scaled.IntPart(), true
}

func moneyAmount(units int64) float64 {
	return decimal.NewFromInt(units).Shift(-moneyScale).InexactFloat64()
}

// exactStringHash makes uniqueness independent of database collation. MySQL
// commonly compares VARCHAR values case-insensitively while PostgreSQL and
// SQLite compare them byte-for-byte; a lowercase SHA-256 hex digest has the
// same equality semantics on every supported database.
func exactStringHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest)
}

func exactTextPredicate(db *gorm.DB, column string) string {
	if db.Dialector.Name() == "mysql" {
		return "BINARY " + column + " = BINARY ?"
	}
	return column + " = ?"
}

func normalizeEarningEntry(entry agent.EarningEntry) (agent.EarningEntry, int64, error) {
	units, ok := moneyUnits(entry.Amount)
	if !ok {
		return agent.EarningEntry{}, 0, agent.ErrEarningInvalid
	}
	entry.Amount = moneyAmount(units)
	if !entry.CreatedAt.IsZero() {
		entry.CreatedAt = entry.CreatedAt.UTC().Truncate(time.Millisecond)
	}
	return entry, units, nil
}

func findPersistedEarning(tx *gorm.DB, entry agent.EarningEntry, idemKey string) (earningRow, error) {
	rawIdentity := "tenant_id = ? AND " + exactTextPredicate(tx, "source_type") +
		" AND " + exactTextPredicate(tx, "source_id")
	query := tx.Where("idem_key_hash = ? OR ("+rawIdentity+")",
		idemKey, entry.TenantID, string(entry.SourceType), entry.SourceID)
	if tx.Dialector.Name() != "sqlite" {
		// MySQL REPEATABLE READ needs a locking/current read after a
		// concurrent no-op insert; a plain snapshot may not see the winner.
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []earningRow
	if err := query.Limit(2).Find(&rows).Error; err != nil {
		return earningRow{}, err
	}
	if len(rows) != 1 {
		return earningRow{}, fmt.Errorf("%w: expected one earning identity row, found %d", errWalletInvariant, len(rows))
	}
	return rows[0], nil
}

// ---- 表 4：agent_withdrawals —— 提现单（状态机 pending→approved/rejected） ----

type withdrawalRow struct {
	ID             int64   `gorm:"column:id;primaryKey;autoIncrement;index:idx_agent_withdrawals_tenant_created_id,priority:3;index:idx_agent_withdrawals_status_created_id,priority:3;index:idx_agent_withdrawals_created_id,priority:2"`
	TenantID       int64   `gorm:"column:tenant_id;not null;index:idx_agent_withdrawals_tenant;index:idx_agent_withdrawals_tenant_created_id,priority:1;uniqueIndex:idx_agent_withdrawals_tenant_request_key_hash,priority:1"`
	UserID         int64   `gorm:"column:user_id;not null;default:0"`
	Amount         float64 `gorm:"column:amount;type:decimal(20,8);not null"`
	AmountUnits    int64   `gorm:"column:amount_units;type:bigint"`
	RequestKey     *string `gorm:"column:request_key;type:varchar(64)"`
	RequestKeyHash *string `gorm:"column:request_key_hash;type:varchar(64);uniqueIndex:idx_agent_withdrawals_tenant_request_key_hash,priority:2"`
	// RequestRemark preserves the immutable request payload used for idempotency
	// comparison. Remark itself remains the review note and may change later.
	RequestRemark *string `gorm:"column:request_remark;type:varchar(255)"`
	Status        string  `gorm:"column:status;type:varchar(16);not null;default:pending;index:idx_agent_withdrawals_status;index:idx_agent_withdrawals_status_created_id,priority:1"`
	Remark        string  `gorm:"column:remark;type:varchar(255);not null;default:''"`
	// PayoutMethod/PayoutAccount/PayoutName/PayoutBank：申请提现那一刻从 agent_profiles 收款账户
	// 整份快照下来的打款目标（提现闭环补强 #1）；记录不可变，日后代理修改收款账户不影响历史单。
	PayoutMethod  string `gorm:"column:payout_method;type:varchar(16);not null;default:''"`
	PayoutAccount string `gorm:"column:payout_account;type:varchar(128);not null;default:''"`
	PayoutName    string `gorm:"column:payout_name;type:varchar(64);not null;default:''"`
	PayoutBank    string `gorm:"column:payout_bank;type:varchar(128);not null;default:''"`
	// PayoutRef 打款单号/凭证；PaidAt 标记已打款时间（mark-paid 时填，提现闭环补强 #2）。
	PayoutRef     string     `gorm:"column:payout_ref;type:varchar(128);not null;default:'';index:idx_agent_withdrawals_payout_ref"`
	PayoutRefHash *string    `gorm:"column:payout_ref_hash;type:varchar(64);index:idx_agent_withdrawals_payout_ref_hash"`
	PaidAt        *time.Time `gorm:"column:paid_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;index:idx_agent_withdrawals_tenant_created_id,priority:2;index:idx_agent_withdrawals_status_created_id,priority:2;index:idx_agent_withdrawals_created_id,priority:1"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at"`
}

func (withdrawalRow) TableName() string { return "agent_withdrawals" }

// payoutRefClaimRow serializes ownership of an external payout reference.
// Keeping this separate from agent_withdrawals also protects installations
// upgraded from a schema where payout_ref had no unique constraint.
type payoutRefClaimRow struct {
	PayoutRefHash string    `gorm:"column:payout_ref_hash;type:varchar(64);primaryKey"`
	PayoutRef     string    `gorm:"column:payout_ref;type:varchar(128);not null"`
	WithdrawalID  int64     `gorm:"column:withdrawal_id;not null;uniqueIndex:idx_agent_payout_ref_claims_v3_withdrawal"`
	CreatedAt     time.Time `gorm:"column:created_at;not null"`
}

func (payoutRefClaimRow) TableName() string { return "agent_payout_ref_claims_v3" }

// v2PayoutRefClaimRow is read-only migration input for the first hash-based
// claim namespace. v4 rebuilds its canonical contents into v3 without
// rewriting or deleting the prior table, so a failed upgrade keeps evidence.
type v2PayoutRefClaimRow struct {
	PayoutRefHash string    `gorm:"column:payout_ref_hash;type:varchar(64);primaryKey"`
	PayoutRef     string    `gorm:"column:payout_ref;type:varchar(128);not null"`
	WithdrawalID  int64     `gorm:"column:withdrawal_id;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;not null"`
}

func (v2PayoutRefClaimRow) TableName() string { return "agent_payout_ref_claims_v2" }

// legacyPayoutRefClaimRow is read-only migration input for the brief
// pre-hash claim schema. It remains in a separate table namespace so a raw
// reference that happens to equal another reference's SHA-256 hex cannot
// collide during an in-place primary-key rewrite.
type legacyPayoutRefClaimRow struct {
	PayoutRef    string    `gorm:"column:payout_ref;type:varchar(128);primaryKey"`
	WithdrawalID int64     `gorm:"column:withdrawal_id;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (legacyPayoutRefClaimRow) TableName() string { return "agent_payout_ref_claims" }

type agentSchemaMigrationRow struct {
	Key         string    `gorm:"column:key;type:varchar(64);primaryKey"`
	ClaimToken  string    `gorm:"column:claim_token;type:varchar(64);not null;default:''"`
	CompletedAt time.Time `gorm:"column:completed_at;not null"`
}

func (agentSchemaMigrationRow) TableName() string { return "agent_schema_migrations" }

const moneyUnitsMigrationKey = "money_units_v1"

// Repo 是 agent.AgentRepo 的 GORM 实现（替换 MemRepo）。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

// 编译期断言：*Repo 满足 agent.AgentRepo 契约。
var _ agent.AgentRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补代理业务表、凭证占用表与迁移标记（含唯一/普通索引）。由 mtwire.Migrate 在 master 节点调用。
func AutoMigrate(db *gorm.DB) error {
	values := []interface{}{
		&profileRow{},
		&walletRow{},
		&earningRow{},
		&withdrawalRow{},
		&payoutRefClaimRow{},
		&agentSchemaMigrationRow{},
	}
	if db.Dialector.Name() == "sqlite" {
		if err := migrateAgentSQLiteAdditively(db, values...); err != nil {
			return err
		}
	} else if err := db.AutoMigrate(values...); err != nil {
		return err
	}
	if err := migrateAgentExactKeyHashes(db); err != nil {
		return err
	}
	if err := installAgentMoneyCompatibilityTriggers(db); err != nil {
		return err
	}
	return backfillAgentMoneyUnits(db)
}

// migrateAgentSQLiteAdditively avoids glebarez/sqlite's table-rebuild path.
// That parser cannot handle the decimal(20,8) DDL emitted for these financial
// tables and fails on a second startup with "invalid DDL, unbalanced brackets".
// Agent schema evolution is additive: create missing tables, add missing
// columns in place, and create missing indexes without rewriting old rows or
// their legacy idempotency keys.
func migrateAgentSQLiteAdditively(db *gorm.DB, values ...interface{}) error {
	for _, value := range values {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			return err
		}
		if !db.Migrator().HasTable(value) {
			if err := db.Migrator().CreateTable(value); err != nil {
				return err
			}
		} else {
			for _, columnName := range statement.Schema.DBNames {
				field := statement.Schema.FieldsByDBName[columnName]
				if field.IgnoreMigration || db.Migrator().HasColumn(value, columnName) {
					continue
				}
				dataType := db.Migrator().FullDataTypeOf(field)
				// SQLite cannot ALTER TABLE ADD a column with an inline UNIQUE
				// constraint. Add the nullable column first; the ParseIndexes loop
				// below creates the named unique index after every column exists.
				dataType.SQL = strings.TrimSuffix(dataType.SQL, " UNIQUE")
				arguments := []interface{}{clause.Table{Name: statement.Table}, clause.Column{Name: columnName}}
				arguments = append(arguments, dataType.Vars...)
				if err := db.Exec("ALTER TABLE ? ADD ? "+dataType.SQL, arguments...).Error; err != nil {
					return fmt.Errorf("add %s.%s: %w", statement.Table, columnName, err)
				}
			}
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if db.Migrator().HasIndex(value, index.Name) {
				continue
			}
			if err := db.Migrator().CreateIndex(value, index.Name); err != nil {
				return fmt.Errorf("create %s index %s: %w", statement.Table, index.Name, err)
			}
		}
	}
	return nil
}

// ---- AgentRepo：代理资料 ----

// SetAgentType 按 tenant_id 主键 upsert 代理资料（设代理 / 改代理复用）。
func (r *Repo) SetAgentType(ctx context.Context, tenantID int64, p agent.AgentParams) error {
	now := r.now()
	row := profileRow{
		TenantID:         tenantID,
		UserID:           p.UserID,
		Level:            p.Level,
		CanAPI:           p.CanAPI,
		CostPriceCNY:     p.CostPrice,
		PackageDiscount:  p.PackageDiscount,
		CommissionRatio:  p.CommissionRatio,
		DiscountFloor:    p.DiscountFloor,
		BottomPriceRatio: p.BottomPriceRatio,
		DiscountRatio:    p.DiscountRatio,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"level", "can_api", "cost_price_cny", "package_discount",
			"commission_ratio", "discount_floor", "bottom_price_ratio", "discount_ratio", "updated_at",
		}),
	}).Create(&row).Error
}

// GetAgentType 读取代理资料；found=false 表示该租户尚未设代理。
// r==nil（仓储未注入，如只测买家主站回落路径的最小 App）时按「未设代理」处理——调用方据此取零折扣/回退，
// 与「主站无代理」的真实语义一致，不 panic。
func (r *Repo) GetAgentType(ctx context.Context, tenantID int64) (agent.AgentParams, bool, error) {
	if r == nil {
		return agent.AgentParams{}, false, nil
	}
	var row profileRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agent.AgentParams{}, false, nil
		}
		return agent.AgentParams{}, false, err
	}
	return agent.AgentParams{
		UserID:           row.UserID,
		CostPrice:        row.CostPriceCNY,
		PackageDiscount:  row.PackageDiscount,
		CommissionRatio:  row.CommissionRatio,
		Level:            row.Level,
		CanAPI:           row.CanAPI,
		DiscountFloor:    row.DiscountFloor,
		BottomPriceRatio: row.BottomPriceRatio,
		DiscountRatio:    row.DiscountRatio,
	}, true, nil
}

// ---- AgentRepo：收款账户（提现闭环补强 #1）----

// GetPayoutAccount 读取代理收款账户；found=false 表示尚未设置（含尚无 profile 行的情形）。
func (r *Repo) GetPayoutAccount(ctx context.Context, tenantID int64) (agent.PayoutAccount, bool, error) {
	var row profileRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agent.PayoutAccount{}, false, nil
		}
		return agent.PayoutAccount{}, false, err
	}
	p := agent.PayoutAccount{
		Method:  agent.PayoutMethod(row.PayoutMethod),
		Account: row.PayoutAccount,
		Name:    row.PayoutName,
		Bank:    row.PayoutBank,
	}
	return p, !p.IsZero(), nil
}

// SetPayoutAccount 按 tenant_id 主键 upsert 代理收款账户；DoUpdates 只列 payout_* 四列 + updated_at，
// 与 SetAgentType 的 DoUpdates 互不重叠列——两者可任意顺序调用，谁都不会清空对方已写入的字段
// （见 profileRow 注释 / TestSetPayoutAccount_DoesNotClobberAgentParams）。
func (r *Repo) SetPayoutAccount(ctx context.Context, tenantID int64, p agent.PayoutAccount) error {
	now := r.now()
	row := profileRow{
		TenantID:      tenantID,
		PayoutMethod:  string(p.Method),
		PayoutAccount: p.Account,
		PayoutName:    p.Name,
		PayoutBank:    p.Bank,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"payout_method", "payout_account", "payout_name", "payout_bank", "updated_at",
		}),
	}).Create(&row).Error
}

// ---- AgentRepo：钱包（只读；写由 AppendEarning / 提现状态机驱动） ----

// GetWallet 返回租户钱包；行不存在返回该租户的零值钱包（不报错），对齐 MemRepo 行为。
func (r *Repo) GetWallet(ctx context.Context, tenantID int64) (*agent.AgentWallet, error) {
	var row walletRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &agent.AgentWallet{TenantID: tenantID}, nil
		}
		return nil, err
	}
	return toWallet(&row), nil
}

type walletMoneyChange struct {
	withdrawable        int64
	frozen              int64
	totalEarned         int64
	requireWithdrawable int64
	requireFrozen       int64
	userID              int64
}

func withMoneyUnitDelta(q *gorm.DB, updates map[string]interface{}, column string, delta int64) *gorm.DB {
	if delta == 0 {
		return q
	}
	updates[column] = gorm.Expr(column+" + ?", delta)
	if delta > 0 {
		return q.Where(column+" <= ?", maxMoneyUnits-delta)
	}
	return q.Where(column+" >= ?", minMoneyUnits-delta)
}

// applyWalletMoneyChange is the single fixed-point balance mutation path.
// The compatibility BEFORE/AFTER trigger derives decimal mirrors in the same
// statement, so every committed row is readable by both current and rollback
// binaries. Missing rows, insufficient funds, overflow, or an unexpected
// RowsAffected value abort the surrounding transaction.
func applyWalletMoneyChange(tx *gorm.DB, tenantID int64, change walletMoneyChange, now time.Time) (bool, error) {
	updates := map[string]interface{}{"updated_at": now}
	q := tx.Model(&walletRow{}).Where("tenant_id = ?", tenantID)
	q = withMoneyUnitDelta(q, updates, "withdrawable_balance_units", change.withdrawable)
	q = withMoneyUnitDelta(q, updates, "frozen_withdraw_amount_units", change.frozen)
	q = withMoneyUnitDelta(q, updates, "total_earned_units", change.totalEarned)
	if change.requireWithdrawable > 0 {
		q = q.Where("withdrawable_balance_units >= ?", change.requireWithdrawable)
	}
	if change.requireFrozen > 0 {
		q = q.Where("frozen_withdraw_amount_units >= ?", change.requireFrozen)
	}
	if change.userID > 0 {
		updates["user_id"] = change.userID
	}

	result := q.Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if result.RowsAffected != 1 {
		return false, fmt.Errorf("%w: tenant %d unit update affected %d rows", errWalletInvariant, tenantID, result.RowsAffected)
	}
	return true, nil
}

// ---- AgentRepo：收益入账（强幂等 + 原子累加） ----

// AppendEarning 幂等入账：先以 idem_key 唯一约束 INSERT（冲突即已入账，applied=false 且不动钱包），
// 首次入账才在同事务 upsert 钱包（withdrawable / total_earned 原子累加）。
func (r *Repo) AppendEarning(ctx context.Context, e agent.EarningEntry) (bool, error) {
	var amountUnits int64
	var err error
	e, amountUnits, err = normalizeEarningEntry(e)
	if err != nil {
		return false, err
	}
	if err := e.Validate(); err != nil {
		return false, err
	}
	now := r.now()
	createdAtProvided := !e.CreatedAt.IsZero()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	applied := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimID := common.GetUUID()
		idemKey := e.IdempotencyKey()
		log := earningRow{
			TenantID:    e.TenantID,
			UserID:      e.UserID,
			SourceType:  string(e.SourceType),
			SourceID:    e.SourceID,
			IdemKey:     idemKey,
			IdemKeyHash: &idemKey,
			ClaimID:     claimID,
			Amount:      e.Amount,
			AmountUnits: amountUnits,
			Remark:      e.Remark,
			CreatedAt:   e.CreatedAt,
		}
		// ON CONFLICT DO NOTHING 捕获精确哈希唯一冲突；随后以 claim_id
		// 回读判定所有权，不依赖各驱动不同的 RowsAffected 语义。
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&log)
		if res.Error != nil {
			return res.Error
		}
		persisted, err := findPersistedEarning(tx, e, idemKey)
		if err != nil {
			return err
		}
		if persisted.TenantID != e.TenantID || persisted.SourceType != string(e.SourceType) || persisted.SourceID != e.SourceID ||
			persisted.UserID != e.UserID || persisted.AmountUnits != amountUnits || persisted.Remark != e.Remark ||
			(createdAtProvided && !persisted.CreatedAt.UTC().Truncate(time.Millisecond).Equal(e.CreatedAt)) {
			return fmt.Errorf("%w: earning idempotency payload mismatch", agent.ErrEarningInvalid)
		}
		if persisted.ClaimID != claimID {
			return nil // exact idempotent replay; applied remains false
		}
		// 首次入账：钱包原子累加（行不存在则创建）。
		w := walletRow{TenantID: e.TenantID, UserID: e.UserID, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&w).Error; err != nil {
			return err
		}
		changed, err := applyWalletMoneyChange(tx, e.TenantID, walletMoneyChange{
			withdrawable: amountUnits,
			totalEarned:  amountUnits,
			userID:       e.UserID,
		}, now)
		if err != nil {
			return err
		}
		if !changed {
			return fmt.Errorf("%w: cannot credit tenant %d", errWalletInvariant, e.TenantID)
		}
		applied = true
		return nil
	})
	return applied, err
}

// AppendEarningsBatch 是 AppendEarning 的批处理版（非接口辅助方法，供 internal/mtwire 异步计费 writer 定时
// flush 调用）：在**单个事务**内逐条 ON CONFLICT DO NOTHING 落 earning 日志（claim_id 回读甄别「首次入账」），
// 按 tenant 合并所有首次入账金额后每租户仅一次 UPSERT 钱包累加。语义与逐条 AppendEarning 完全一致
// （同 idem_key 不重复增余额；负数 manual_adjustment 亦可），仅把「N 事务 / N 次 agent_wallets 热行 UPSERT」
// 压成「1 事务 / 每租户 1 次 UPSERT」——消除自研写放大与钱包热行的跨请求行锁争用。
//
// 返回本批「首次入账」条数。任一步 DB 异常整批回滚（applied=0），由调用方按幂等安全重排/重试——幂等键去重
// 使重排绝不双计。钱包 user_id 采该租户本批**最后一条**首次入账的 owner，与逐条 UPSERT 的「后写覆盖」等价。
func (r *Repo) AppendEarningsBatch(ctx context.Context, entries []agent.EarningEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	now := r.now()
	type tenantAcc struct {
		sum    int64
		userID int64
	}
	applied := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		accs := make(map[int64]*tenantAcc)
		order := make([]int64, 0, len(entries)) // 稳定 UPSERT 顺序（便于复现/审计）
		for _, e := range entries {
			var amountUnits int64
			var err error
			e, amountUnits, err = normalizeEarningEntry(e)
			if err != nil {
				return err
			}
			if err := e.Validate(); err != nil {
				return err
			}
			createdAtProvided := !e.CreatedAt.IsZero()
			if e.CreatedAt.IsZero() {
				e.CreatedAt = now
			}
			claimID := common.GetUUID()
			idemKey := e.IdempotencyKey()
			log := earningRow{
				TenantID:    e.TenantID,
				UserID:      e.UserID,
				SourceType:  string(e.SourceType),
				SourceID:    e.SourceID,
				IdemKey:     idemKey,
				IdemKeyHash: &idemKey,
				ClaimID:     claimID,
				Amount:      e.Amount,
				AmountUnits: amountUnits,
				Remark:      e.Remark,
				CreatedAt:   e.CreatedAt,
			}
			// ON CONFLICT DO NOTHING 吞掉精确哈希幂等冲突；走到
			// res.Error 的是意外错误 → 整批回滚重排。
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&log)
			if res.Error != nil {
				return res.Error
			}
			persisted, err := findPersistedEarning(tx, e, idemKey)
			if err != nil {
				return err
			}
			if persisted.TenantID != e.TenantID || persisted.SourceType != string(e.SourceType) || persisted.SourceID != e.SourceID ||
				persisted.UserID != e.UserID || persisted.AmountUnits != amountUnits || persisted.Remark != e.Remark ||
				(createdAtProvided && !persisted.CreatedAt.UTC().Truncate(time.Millisecond).Equal(e.CreatedAt)) {
				return fmt.Errorf("%w: earning idempotency payload mismatch", agent.ErrEarningInvalid)
			}
			if persisted.ClaimID != claimID {
				continue
			}
			a, ok := accs[e.TenantID]
			if !ok {
				a = &tenantAcc{}
				accs[e.TenantID] = a
				order = append(order, e.TenantID)
			}
			if (amountUnits > 0 && a.sum > maxMoneyUnits-amountUnits) ||
				(amountUnits < 0 && a.sum < minMoneyUnits-amountUnits) {
				return agent.ErrEarningInvalid
			}
			a.sum += amountUnits
			a.userID = e.UserID
			applied++
		}
		// 每租户仅一次钱包 UPSERT（累加合并金额）——与 AppendEarning 单条 UPSERT 同列同表达式，仅合并了 N→1。
		for _, tenantID := range order {
			a := accs[tenantID]
			w := walletRow{TenantID: tenantID, UserID: a.userID, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&w).Error; err != nil {
				return err
			}
			if a.sum == 0 {
				if err := tx.Model(&walletRow{}).Where("tenant_id = ?", tenantID).
					Updates(map[string]interface{}{"user_id": a.userID, "updated_at": now}).Error; err != nil {
					return err
				}
				continue
			}
			changed, err := applyWalletMoneyChange(tx, tenantID, walletMoneyChange{
				withdrawable: a.sum,
				totalEarned:  a.sum,
				userID:       a.userID,
			}, now)
			if err != nil {
				return err
			}
			if !changed {
				return fmt.Errorf("%w: cannot credit tenant %d", errWalletInvariant, tenantID)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return applied, nil
}

// ---- AgentRepo：提现状态机（原子冻结 / 审核翻牌 + 资金守恒） ----

// CreateWithdrawal 原子冻结可提现余额并建 pending 提现单。
// 金额非正 -> ErrWithdrawInsufficient；条件 UPDATE 0 行（余额不足 / 无钱包）-> ErrWithdrawInsufficient。
func (r *Repo) CreateWithdrawal(ctx context.Context, wd *agent.Withdrawal) error {
	wd.RequestKey = strings.TrimSpace(wd.RequestKey)
	if utf8.RuneCountInString(wd.RequestKey) > agent.MaxWithdrawalRequestKeyLength {
		return agent.ErrWithdrawRequestKeyInvalid
	}
	amountUnits, ok := moneyUnits(wd.Amount)
	if !ok || amountUnits <= 0 {
		return agent.ErrWithdrawInsufficient
	}
	if amountUnits%moneyCentUnits != 0 {
		return agent.ErrWithdrawAmountInvalid
	}
	wd.Amount = moneyAmount(amountUnits)
	if wd.RequestKey != "" {
		found, err := replayWithdrawalByRequestKey(r.db.WithContext(ctx), wd, amountUnits)
		if err != nil || found {
			return err
		}
	}
	now := r.now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 原子冻结：可提现 -= amount、冻结 += amount，仅当可提现充足。
		changed, err := applyWalletMoneyChange(tx, wd.TenantID, walletMoneyChange{
			withdrawable:        -amountUnits,
			frozen:              amountUnits,
			requireWithdrawable: amountUnits,
		}, now)
		if err != nil {
			return err
		}
		if !changed {
			return agent.ErrWithdrawInsufficient
		}
		row := withdrawalRow{
			TenantID:      wd.TenantID,
			UserID:        wd.UserID,
			Amount:        wd.Amount,
			AmountUnits:   amountUnits,
			Status:        string(agent.WithdrawPending),
			Remark:        wd.Remark,
			PayoutMethod:  string(wd.PayoutMethod),
			PayoutAccount: wd.PayoutAccount,
			PayoutName:    wd.PayoutName,
			PayoutBank:    wd.PayoutBank,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if wd.RequestKey != "" {
			requestKeyHash := exactStringHash(wd.RequestKey)
			row.RequestKey = &wd.RequestKey
			row.RequestKeyHash = &requestKeyHash
			row.RequestRemark = &wd.Remark
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		wd.ID = row.ID
		wd.Status = agent.WithdrawPending
		wd.CreatedAt = now
		wd.UpdatedAt = now
		return nil
	})
	if err == nil || wd.RequestKey == "" {
		return err
	}

	// A concurrent request may have won the unique (tenant_id, request_key)
	// insert after our optimistic lookup. The failed transaction has already
	// rolled back its wallet freeze (important for PostgreSQL, where a unique
	// violation aborts the transaction), so resolve the race using a fresh
	// transaction-visible lookup. Do not infer ownership from RowsAffected.
	found, replayErr := replayWithdrawalByRequestKey(r.db.WithContext(ctx), wd, amountUnits)
	if replayErr != nil || found {
		return replayErr
	}
	return err
}

// replayWithdrawalByRequestKey returns the original complete withdrawal for
// an exact retry, or a conflict when the same tenant-scoped key is reused with
// a different caller-controlled payload. Payout fields are intentionally not
// compared: they are immutable server-side snapshots captured by the first
// accepted request and must be replayed as originally persisted.
func replayWithdrawalByRequestKey(db *gorm.DB, wd *agent.Withdrawal, amountUnits int64) (bool, error) {
	requestKeyHash := exactStringHash(wd.RequestKey)
	rawPredicate := exactTextPredicate(db, "request_key")
	var rows []withdrawalRow
	if err := db.Where("tenant_id = ? AND (request_key_hash = ? OR "+rawPredicate+")",
		wd.TenantID, requestKeyHash, wd.RequestKey).Limit(2).Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	if len(rows) != 1 {
		return true, fmt.Errorf("%w: duplicate withdrawal request identity", errWalletInvariant)
	}
	row := rows[0]
	if row.RequestKey == nil || *row.RequestKey != wd.RequestKey {
		return true, fmt.Errorf("%w: withdrawal request-key hash collision", errWalletInvariant)
	}
	requestRemark := row.Remark
	if row.RequestRemark != nil {
		requestRemark = *row.RequestRemark
	}
	if row.UserID != wd.UserID || row.AmountUnits != amountUnits || requestRemark != wd.Remark {
		return true, agent.ErrWithdrawIdempotencyConflict
	}
	*wd = *toWithdrawal(&row)
	return true, nil
}

// GetWithdrawal 按 id 读取提现单；不存在返回 ErrWithdrawNotFound。
func (r *Repo) GetWithdrawal(ctx context.Context, id int64) (*agent.Withdrawal, error) {
	var row withdrawalRow
	err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, agent.ErrWithdrawNotFound
		}
		return nil, err
	}
	return toWithdrawal(&row), nil
}

// ResolveWithdrawal 原子迁移 pending→{approved,rejected}（Review 的唯二调用目标）并按新钱流规则
// 移动资金（提现闭环补强 #2 钱流调整）：approved **不动钱**（仅记决策，钱仍在 frozen，等
// MarkWithdrawalPaid 才真正出账）；rejected 解冻退回可提现。
// 不存在 -> ErrWithdrawNotFound；非 pending（或并发已被审核）-> ErrWithdrawNotPending。
func (r *Repo) ResolveWithdrawal(ctx context.Context, id int64, target agent.WithdrawStatus, remark string) error {
	now := r.now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row withdrawalRow
		rowQuery := tx
		if tx.Dialector.Name() != "sqlite" {
			rowQuery = rowQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := rowQuery.Take(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agent.ErrWithdrawNotFound
			}
			return err
		}
		if !agent.WithdrawStatus(row.Status).CanTransitionTo(target) {
			return agent.ErrWithdrawNotPending
		}
		if row.AmountUnits <= 0 {
			return fmt.Errorf("%w: withdrawal %d has invalid amount units", errWalletInvariant, id)
		}
		// Approval must not legitimize an already-corrupt withdrawal. Lock and
		// validate its funding before the status CAS; rejected uses the same
		// precondition before refunding. SQLite serializes the following write,
		// while MySQL/PostgreSQL hold an explicit row lock through commit.
		var wallet struct {
			FrozenUnits int64 `gorm:"column:frozen_withdraw_amount_units"`
		}
		walletQuery := tx.Model(&walletRow{}).
			Select("frozen_withdraw_amount_units").
			Where("tenant_id = ?", row.TenantID)
		if tx.Dialector.Name() != "sqlite" {
			walletQuery = walletQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := walletQuery.Take(&wallet).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: withdrawal %d wallet is missing", errWalletInvariant, id)
			}
			return err
		}
		if wallet.FrozenUnits < row.AmountUnits {
			return fmt.Errorf("%w: withdrawal %d frozen balance is insufficient", errWalletInvariant, id)
		}
		// CAS：pending→target，抢到者负责移动资金；并发败者 RowsAffected==0。
		res := tx.Model(&withdrawalRow{}).
			Where("id = ? AND status = ?", id, string(agent.WithdrawPending)).
			Updates(map[string]interface{}{
				"status":      string(target),
				"remark":      remark,
				"reviewed_at": now,
				"updated_at":  now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return agent.ErrWithdrawNotPending
		}
		if target != agent.WithdrawRejected {
			return nil // approved：不触碰钱包，钱仍在 frozen
		}
		// rejected：解冻退回可提现（frozen → withdrawable，金额守恒复原）。
		changed, err := applyWalletMoneyChange(tx, row.TenantID, walletMoneyChange{
			withdrawable:  row.AmountUnits,
			frozen:        -row.AmountUnits,
			requireFrozen: row.AmountUnits,
		}, now)
		if err != nil {
			return err
		}
		if !changed {
			return fmt.Errorf("%w: cannot reject withdrawal %d", errWalletInvariant, id)
		}
		return nil
	})
}

// MarkWithdrawalPaid 原子迁移 approved→paid（CAS，WHERE status='approved'）并扣减冻结资金
// （线下打款真正出账），记 payout_ref/paid_at（提现闭环补强 #2）。与 ResolveWithdrawal 同一 txn 风格：
// 先读行校验状态机合法性，再 CAS UPDATE 抢状态，赢家才移动资金；并发败者 RowsAffected==0。
// 不存在 -> ErrWithdrawNotFound；非 approved（或并发已被标记）-> ErrWithdrawNotApproved。
func (r *Repo) MarkWithdrawalPaid(ctx context.Context, id int64, payoutRef string) error {
	payoutRef = strings.TrimSpace(payoutRef)
	if payoutRef == "" {
		return agent.ErrPayoutRefRequired
	}
	if utf8.RuneCountInString(payoutRef) > agent.MaxPayoutRefLength {
		return agent.ErrPayoutRefInvalid
	}
	now := r.now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row withdrawalRow
		rowQuery := tx
		if tx.Dialector.Name() != "sqlite" {
			rowQuery = rowQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := rowQuery.Take(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agent.ErrWithdrawNotFound
			}
			return err
		}
		if !agent.WithdrawStatus(row.Status).CanTransitionTo(agent.WithdrawPaid) {
			return agent.ErrWithdrawNotApproved
		}

		payoutRefHash := exactStringHash(payoutRef)
		// Upgraded databases may already contain paid withdrawals created before
		// payout-reference claims existed. Hashes are the normal indexed path;
		// the exact raw fallback also covers stale old-node writes or a corrupt
		// missing/mismatched hash without inheriting MySQL's case-insensitive
		// collation.
		var historical withdrawalRow
		rawPredicate := exactTextPredicate(tx, "payout_ref")
		historyWhere := "id <> ? AND (payout_ref_hash = ? OR " + rawPredicate + ")"
		err := tx.Select("id").Where(historyWhere, id, payoutRefHash, payoutRef).Take(&historical).Error
		if err == nil {
			return agent.ErrPayoutRefDuplicate
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// Claim with INSERT ... ON CONFLICT DO NOTHING, then read ownership back.
		// The read, rather than RowsAffected, is authoritative under MySQL
		// clientFoundRows and all supported dialects. PostgreSQL also keeps the
		// transaction usable because the duplicate is handled by ON CONFLICT.
		claim := payoutRefClaimRow{PayoutRefHash: payoutRefHash, PayoutRef: payoutRef, WithdrawalID: id, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim).Error; err != nil {
			return err
		}
		var owned payoutRefClaimRow
		ownedByRef := tx.Where("payout_ref_hash = ?", payoutRefHash)
		if tx.Dialector.Name() != "sqlite" {
			ownedByRef = ownedByRef.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := ownedByRef.Take(&owned).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// ON CONFLICT may have lost on UNIQUE(withdrawal_id), not the
			// reference primary key. Read that ownership explicitly so a same
			// withdrawal/different-reference race returns a domain conflict rather
			// than leaking a bare gorm.ErrRecordNotFound.
			ownedByWithdrawal := tx.Where("withdrawal_id = ?", id)
			if tx.Dialector.Name() != "sqlite" {
				ownedByWithdrawal = ownedByWithdrawal.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if claimErr := ownedByWithdrawal.Take(&owned).Error; claimErr == nil {
				return agent.ErrWithdrawNotApproved
			} else if !errors.Is(claimErr, gorm.ErrRecordNotFound) {
				return claimErr
			}
			return fmt.Errorf("%w: payout reference claim disappeared for withdrawal %d", errWalletInvariant, id)
		}
		if owned.PayoutRef != payoutRef {
			return fmt.Errorf("%w: payout reference hash collision", errWalletInvariant)
		}
		if owned.WithdrawalID != id {
			return agent.ErrPayoutRefDuplicate
		}
		// CAS：approved→paid，抢到者负责移动资金；并发败者 RowsAffected==0。
		res := tx.Model(&withdrawalRow{}).
			Where("id = ? AND status = ?", id, string(agent.WithdrawApproved)).
			Updates(map[string]interface{}{
				"status":          string(agent.WithdrawPaid),
				"payout_ref":      payoutRef,
				"payout_ref_hash": payoutRefHash,
				"paid_at":         now,
				"updated_at":      now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return agent.ErrWithdrawNotApproved
		}
		if row.AmountUnits <= 0 {
			return fmt.Errorf("%w: withdrawal %d has invalid amount units", errWalletInvariant, id)
		}
		// 打款真正出账：扣冻结（资金离开系统）。
		changed, err := applyWalletMoneyChange(tx, row.TenantID, walletMoneyChange{
			frozen:        -row.AmountUnits,
			requireFrozen: row.AmountUnits,
		}, now)
		if err != nil {
			return err
		}
		if !changed {
			return fmt.Errorf("%w: cannot pay withdrawal %d", errWalletInvariant, id)
		}
		return nil
	})
}

// ---- 非接口辅助方法（供 mtwire handler / seed 装配；不属 AgentRepo 契约） ----

// EnsureWallet 幂等地为某租户建一个零值钱包（账户已存在则不动）。供 seed / 设代理。
func (r *Repo) EnsureWallet(ctx context.Context, tenantID, userID int64) error {
	row := walletRow{TenantID: tenantID, UserID: userID, UpdatedAt: r.now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// AgentRow 是「设代理列表」的一行原始资料（tenant_id + owner + 类型/参数）。
type AgentRow struct {
	TenantID         int64
	UserID           int64
	Level            int
	CanAPI           bool
	CostPriceCNY     float64
	PackageDiscount  float64
	CommissionRatio  float64
	BottomPriceRatio float64
	DiscountRatio    float64
}

// ListProfiles 列出全部代理资料（每行 = 一个代理租户），供管理端 GET /api/admin/agents 装配。
func (r *Repo) ListProfiles(ctx context.Context) ([]AgentRow, error) {
	var rows []profileRow
	if err := r.db.WithContext(ctx).Order("tenant_id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AgentRow, 0, len(rows))
	for _, p := range rows {
		out = append(out, AgentRow{
			TenantID:         p.TenantID,
			UserID:           p.UserID,
			Level:            p.Level,
			CanAPI:           p.CanAPI,
			CostPriceCNY:     p.CostPriceCNY,
			PackageDiscount:  p.PackageDiscount,
			CommissionRatio:  p.CommissionRatio,
			BottomPriceRatio: p.BottomPriceRatio,
			DiscountRatio:    p.DiscountRatio,
		})
	}
	return out, nil
}

const (
	defaultWithdrawalPageSize = 20
	maxWithdrawalPageSize     = 100
)

func normalizeWithdrawalPaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultWithdrawalPageSize
	} else if pageSize > maxWithdrawalPageSize {
		pageSize = maxWithdrawalPageSize
	}
	return page, pageSize
}

// ListWithdrawalsByTenantPage 分页列出某租户的提现单（创建时间、ID 倒序）。
func (r *Repo) ListWithdrawalsByTenantPage(ctx context.Context, tenantID int64, page, pageSize int) ([]agent.Withdrawal, int64, error) {
	page, pageSize = normalizeWithdrawalPaging(page, pageSize)
	q := r.db.WithContext(ctx).Model(&withdrawalRow{}).Where("tenant_id = ?", tenantID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return []agent.Withdrawal{}, total, nil
	}
	var rows []withdrawalRow
	if err := q.Order("created_at desc, id desc").
		Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return toWithdrawals(rows), total, nil
}

// ListWithdrawalsByTenant 保留给不认识分页响应的旧调用方。它优先保留仍需处理的
// pending/approved，再按时间取满 100 条，避免大量新终态记录把旧活跃单挤出视野。
func (r *Repo) ListWithdrawalsByTenant(ctx context.Context, tenantID int64) ([]agent.Withdrawal, error) {
	var rows []withdrawalRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("CASE WHEN status IN ('pending','approved') THEN 0 ELSE 1 END asc").
		Order("created_at desc, id desc").Limit(maxWithdrawalPageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return toWithdrawals(rows), nil
}

// ListWithdrawalsPage 分页列出全部提现单；status 非空时按状态过滤。
func (r *Repo) ListWithdrawalsPage(ctx context.Context, status string, page, pageSize int) ([]agent.Withdrawal, int64, error) {
	page, pageSize = normalizeWithdrawalPaging(page, pageSize)
	q := r.db.WithContext(ctx).Model(&withdrawalRow{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return []agent.Withdrawal{}, total, nil
	}
	var rows []withdrawalRow
	if err := q.Order("created_at desc, id desc").
		Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return toWithdrawals(rows), total, nil
}

// ListWithdrawals 保留给不认识分页响应的旧调用方，活跃单优先且最多返回 100 条。
func (r *Repo) ListWithdrawals(ctx context.Context, status string) ([]agent.Withdrawal, error) {
	q := r.db.WithContext(ctx).Model(&withdrawalRow{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []withdrawalRow
	if err := q.Order("CASE WHEN status IN ('pending','approved') THEN 0 ELSE 1 END asc").
		Order("created_at desc, id desc").Limit(maxWithdrawalPageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return toWithdrawals(rows), nil
}

// earningsListCap 是自助收益台账「最新 N 条」明细流的安全上限。agent_earning_logs 每笔计费请求线性增长
// （月级可达百万行），无界 Find 会 OOM/长阻塞；此处只截最新 N 条（顶部汇总卡取自钱包聚合、不受影响），
// 完整分页历史走 GET /api/tenant/finance/detail（DetailEarnings，page + CSV 导出），一条不丢。
const earningsListCap = 1000

// ListEarningsByTenant 列出某租户的收益台账（按时间倒序，上限最新 earningsListCap 条）。供代理自助 GET /api/tenant/earnings。
func (r *Repo) ListEarningsByTenant(ctx context.Context, tenantID int64) ([]agent.EarningEntry, error) {
	var rows []earningRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("created_at desc").Limit(earningsListCap).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agent.EarningEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, agent.EarningEntry{
			TenantID:   e.TenantID,
			UserID:     e.UserID,
			SourceType: agent.EarningSource(e.SourceType),
			SourceID:   e.SourceID,
			Amount:     moneyAmount(e.AmountUnits),
			Remark:     e.Remark,
			CreatedAt:  e.CreatedAt,
		})
	}
	return out, nil
}

// ---- 映射辅助 ----

func toWallet(row *walletRow) *agent.AgentWallet {
	return &agent.AgentWallet{
		TenantID:             row.TenantID,
		UserID:               row.UserID,
		APIBalance:           moneyAmount(row.APIBalanceUnits),
		WithdrawableBalance:  moneyAmount(row.WithdrawableBalanceUnits),
		FrozenWithdrawAmount: moneyAmount(row.FrozenWithdrawAmountUnits),
		TotalEarned:          moneyAmount(row.TotalEarnedUnits),
		UpdatedAt:            row.UpdatedAt,
	}
}

func toWithdrawal(row *withdrawalRow) *agent.Withdrawal {
	w := &agent.Withdrawal{
		ID:            row.ID,
		TenantID:      row.TenantID,
		UserID:        row.UserID,
		Amount:        moneyAmount(row.AmountUnits),
		Status:        agent.WithdrawStatus(row.Status),
		Remark:        row.Remark,
		PayoutMethod:  agent.PayoutMethod(row.PayoutMethod),
		PayoutAccount: row.PayoutAccount,
		PayoutName:    row.PayoutName,
		PayoutBank:    row.PayoutBank,
		PayoutRef:     row.PayoutRef,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	if row.RequestKey != nil {
		w.RequestKey = *row.RequestKey
	}
	if row.ReviewedAt != nil {
		w.ReviewedAt = *row.ReviewedAt
	}
	if row.PaidAt != nil {
		w.PaidAt = *row.PaidAt
	}
	return w
}

func toWithdrawals(rows []withdrawalRow) []agent.Withdrawal {
	out := make([]agent.Withdrawal, 0, len(rows))
	for i := range rows {
		out = append(out, *toWithdrawal(&rows[i]))
	}
	return out
}
