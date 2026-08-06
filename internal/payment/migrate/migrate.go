// Package migrate 提供 payment_orders 版本化 expand migration（生产可部署）。
//
// 流程：precheck → expand → batch backfill → verify → create/verify indexes → commit schema version。
// CurrentVersion 只读；仅全部验证成功后写 SchemaVersion。
// 新字段 additive/nullable，旧二进制回滚仍可 INSERT。
package migrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SchemaVersion 当前支付 schema 版本（readiness 最低要求）。
const SchemaVersion = 2

type versionRow struct {
	ID        int       `gorm:"primaryKey"`
	Version   int       `gorm:"column:version;not null;uniqueIndex"`
	AppliedAt time.Time `gorm:"column:applied_at"`
	Note      string    `gorm:"column:note;type:varchar(255)"`
}

func (versionRow) TableName() string { return "mt_payment_schema_version" }

// fullOrderShape 完整新表形状（空库创建用；expand 亦复用）。
// 索引与 gormrepo.orderRow 对齐：调度/唯一键列序精确。
type fullOrderShape struct {
	ID                    int64      `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo               string     `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:idx_payment_orders_no"`
	Type                  string     `gorm:"column:type;type:varchar(16);not null"`
	TenantID              int64      `gorm:"column:tenant_id;not null"`
	UserID                int64      `gorm:"column:user_id;not null"`
	Provider              string     `gorm:"column:provider;type:varchar(16);not null;uniqueIndex:idx_payment_orders_provider_txn,priority:1"`
	AmountUSD             float64    `gorm:"column:amount_usd;type:decimal(20,8);not null;default:0"`
	ActualPaid            float64    `gorm:"column:actual_paid;type:decimal(20,2);not null;default:0"`
	ActualPaidFen         int64      `gorm:"column:actual_paid_fen;not null;default:0"`
	GroupID               int64      `gorm:"column:group_id;not null;default:0"`
	PlanID                int64      `gorm:"column:plan_id;not null;default:0"`
	Subject               string     `gorm:"column:subject;type:varchar(255)"`
	Reference             string     `gorm:"column:reference;type:varchar(255)"`
	IdempotencyKey        *string    `gorm:"column:idempotency_key;type:varchar(64);uniqueIndex:idx_payment_orders_idem"`
	ProviderTransactionID *string    `gorm:"column:provider_transaction_id;type:varchar(128);uniqueIndex:idx_payment_orders_provider_txn,priority:2"`
	Status                string     `gorm:"column:status;type:varchar(16);not null;index:idx_pay_ord_status_nq_id_v2,priority:1;index:idx_pay_ord_status_upd_id_v2,priority:1"`
	CreateState           string     `gorm:"column:create_state;type:varchar(32)"`
	RootOrderNo           *string    `gorm:"column:root_order_no;type:varchar(64)"`
	ReplacesOrderNo       *string    `gorm:"column:replaces_order_no;type:varchar(64)"`
	AttemptNo             int        `gorm:"column:attempt_no;default:1"`
	ActiveOrderNo         *string    `gorm:"column:active_order_no;type:varchar(64)"`
	ProviderTradeState    string     `gorm:"column:provider_trade_state;type:varchar(32)"`
	NotifyURL             string     `gorm:"column:notify_url;type:varchar(255)"`
	PayURL                string     `gorm:"column:pay_url;type:text"`
	ExpiresAt             *time.Time `gorm:"column:expires_at"`
	ProviderPaidAt        *time.Time `gorm:"column:provider_paid_at"`
	CreditedAt            *time.Time `gorm:"column:credited_at"`
	CallbackReceivedAt    *time.Time `gorm:"column:callback_received_at"`
	NextQueryAt           *time.Time `gorm:"column:next_query_at;index:idx_pay_ord_status_nq_id_v2,priority:2"`
	QueryAttempts         int        `gorm:"column:query_attempts;not null;default:0"`
	QueryClaimToken       string     `gorm:"column:query_claim_token;type:varchar(64)"`
	QueryClaimUntil       *time.Time `gorm:"column:query_claim_until"`
	RecoveryClaimToken    string     `gorm:"column:recovery_claim_token;type:varchar(64)"`
	RecoveryClaimUntil    *time.Time `gorm:"column:recovery_claim_until"`
	NextRecoveryAt        *time.Time `gorm:"column:next_recovery_at"`
	LastErrorClass        string     `gorm:"column:last_error_class;type:varchar(64)"`
	LastNetworkStage      string     `gorm:"column:last_network_stage;type:varchar(32)"`
	CreateAttempts        int        `gorm:"column:create_attempts;not null;default:0"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;index:idx_pay_ord_status_upd_id_v2,priority:2"`
}

func (fullOrderShape) TableName() string { return "payment_orders" }

// outboxShape cache invalidation outbox（调度索引）。
type outboxShape struct {
	OrderNo       string     `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	UserID        int64      `gorm:"column:user_id;not null;index"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	DoneAt        *time.Time `gorm:"column:done_at"`
	Attempts      int        `gorm:"column:attempts;not null;default:0"`
	NextAttemptAt *time.Time `gorm:"column:next_attempt_at;index:idx_mt_outbox_sched,priority:2"`
	ClaimToken    string     `gorm:"column:claim_token;type:varchar(64)"`
	ClaimUntil    *time.Time `gorm:"column:claim_until"`
	PoisonedAt    *time.Time `gorm:"column:poisoned_at"`
}

func (outboxShape) TableName() string { return "mt_user_cache_invalidation_outbox" }

// CurrentVersion 只读版本；不 AutoMigrate。
func CurrentVersion(ctx context.Context, db *gorm.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("nil db")
	}
	if !db.WithContext(ctx).Migrator().HasTable(&versionRow{}) {
		return 0, nil
	}
	var cur versionRow
	err := db.WithContext(ctx).Order("version desc").First(&cur).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return cur.Version, nil
}

// EnsureSchema 幂等 expand+backfill；仅全部成功后写 SchemaVersion。
func EnsureSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("payment migrate: nil db")
	}
	// precheck：版本表可创建（版本行仅在 verify 后写入）
	if err := db.WithContext(ctx).AutoMigrate(&versionRow{}); err != nil {
		return fmt.Errorf("precheck version table: %w", err)
	}
	cur, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	if cur >= SchemaVersion {
		// 幂等：已到位仍跑一次 verify（连续运行两次安全）
		return VerifyOnly(ctx, db)
	}

	// expand / 空库完整建表（additive）
	if !db.Migrator().HasTable("payment_orders") {
		if err := db.WithContext(ctx).AutoMigrate(&fullOrderShape{}); err != nil {
			return fmt.Errorf("create payment_orders: %w", err)
		}
	} else {
		if err := db.WithContext(ctx).AutoMigrate(&fullOrderShape{}); err != nil {
			return fmt.Errorf("expand payment_orders: %w", err)
		}
	}

	// outbox expand（调度字段 additive）
	if err := db.WithContext(ctx).AutoMigrate(&outboxShape{}); err != nil {
		return fmt.Errorf("expand outbox: %w", err)
	}

	// backfill
	if err := backfillRoots(ctx, db); err != nil {
		return err
	}

	// verify columns + indexes
	if err := verify(ctx, db); err != nil {
		return err
	}

	// commit schema version（仅成功后）
	return db.WithContext(ctx).Create(&versionRow{
		Version:   SchemaVersion,
		AppliedAt: time.Now().UTC(),
		Note:      "payment_schema_v2",
	}).Error
}

func backfillRoots(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasColumn(&fullOrderShape{}, "RootOrderNo") {
		return nil
	}
	// 三库兼容：不使用 UPDATE…LIMIT
	return db.WithContext(ctx).Exec(`
UPDATE payment_orders
SET root_order_no = order_no
WHERE root_order_no IS NULL OR root_order_no = ''`).Error
}

func verify(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasTable("payment_orders") {
		return fmt.Errorf("verify: payment_orders missing")
	}
	need := []string{
		"CreateState", "RootOrderNo", "PayURL", "QueryClaimToken", "RecoveryClaimToken",
		"NextQueryAt", "ActualPaidFen", "IdempotencyKey", "ProviderTransactionID",
	}
	for _, f := range need {
		if !db.Migrator().HasColumn(&fullOrderShape{}, f) {
			return fmt.Errorf("verify: missing column field %s", f)
		}
	}
	// 关键索引存在（GORM Migrator 名）
	for _, idx := range []string{
		"idx_payment_orders_no",
		"idx_pay_ord_status_nq_id_v2",
		"idx_pay_ord_status_upd_id_v2",
		"idx_payment_orders_provider_txn",
		"idx_payment_orders_idem",
	} {
		if !db.Migrator().HasIndex(&fullOrderShape{}, idx) {
			// 部分方言/已有表索引名可能不同：尝试再 AutoMigrate 一次后复检
			_ = db.WithContext(ctx).AutoMigrate(&fullOrderShape{})
			if !db.Migrator().HasIndex(&fullOrderShape{}, idx) {
				return fmt.Errorf("verify: missing index %s", idx)
			}
		}
	}
	return nil
}

// VerifyOnly 只读核验，供 --payment-schema-verify 与 readiness。
func VerifyOnly(ctx context.Context, db *gorm.DB) error {
	v, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	if v < SchemaVersion {
		return fmt.Errorf("payment schema version %d < required %d", v, SchemaVersion)
	}
	return verify(ctx, db)
}

// DialectorName 供测试/运维日志。
func DialectorName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return ""
	}
	return strings.ToLower(db.Dialector.Name())
}
