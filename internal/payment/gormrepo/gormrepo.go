// Package gormrepo 用真实 GORM 实现 payment.OrderRepo（支付订单持久化）。
//
// 单表 payment_orders（detailed-design §6.2）：
//   - order_no 唯一索引 —— 下单幂等键 + 重复回调定位键；
//   - idempotency_key 可空唯一 —— 客户端支付意图幂等（PAY-IDEM-01）；
//   - (provider, provider_transaction_id) 可空唯一 —— 支付机构交易号（PAY-FACT-01）；
//   - status 状态机用条件 UPDATE 做原子 CAS；
//   - (status, next_query_at, id) 调度索引 —— 5s/30s/60s 查单（PAY-REC-01/02）。
//
// 金额列：USD 入账用 decimal(20,8)；实付 ¥ 用 decimal(20,2)（报表兼容）+ actual_paid_fen（精确比对）。
package gormrepo

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

func cryptoRandRead(b []byte) (int, error) { return cryptorand.Read(b) }

// orderRow 是 payment_orders 表的 GORM 模型（Phase E expand-only 字段）。
type orderRow struct {
	ID                    int64   `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo               string  `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:idx_payment_orders_no"`
	Type                  string  `gorm:"column:type;type:varchar(16);not null;index:idx_payment_orders_type"`
	TenantID              int64   `gorm:"column:tenant_id;not null;index:idx_payment_orders_tenant_user,priority:1"`
	UserID                int64   `gorm:"column:user_id;not null;index:idx_payment_orders_tenant_user,priority:2"`
	Provider              string  `gorm:"column:provider;type:varchar(16);not null;uniqueIndex:idx_payment_orders_provider_txn,priority:1"`
	AmountUSD             float64 `gorm:"column:amount_usd;type:decimal(20,8);not null;default:0"`
	ActualPaid            float64 `gorm:"column:actual_paid;type:decimal(20,2);not null;default:0"`
	ActualPaidFen         int64   `gorm:"column:actual_paid_fen;not null;default:0"`
	GroupID               int64   `gorm:"column:group_id;not null;default:0"`
	PlanID                int64   `gorm:"column:plan_id;not null;default:0"`
	Subject               string  `gorm:"column:subject;type:varchar(255)"`
	Reference             string  `gorm:"column:reference;type:varchar(255)"`
	IdempotencyKey        *string `gorm:"column:idempotency_key;type:varchar(64);uniqueIndex:idx_payment_orders_idem"`
	ProviderTransactionID *string `gorm:"column:provider_transaction_id;type:varchar(128);uniqueIndex:idx_payment_orders_provider_txn,priority:2"`
	Status                string  `gorm:"column:status;type:varchar(16);not null;index:idx_pay_ord_status_nq_id_v2,priority:1;index:idx_pay_ord_status_upd_id_v2,priority:1"`
	// Phase F：存量安全 expand——root/create_state 先可空，由版本化 backfill 后再收紧。
	// 禁止直接 not null + 复合唯一在存量 34 行上 AutoMigrate 失败。
	CreateState        string  `gorm:"column:create_state;type:varchar(32);default:local_created"`
	RootOrderNo        *string `gorm:"column:root_order_no;type:varchar(64);index:idx_pay_ord_root_v2"`
	ReplacesOrderNo    *string `gorm:"column:replaces_order_no;type:varchar(64);uniqueIndex:idx_pay_ord_replaces_v2"`
	AttemptNo          int     `gorm:"column:attempt_no;not null;default:1"`
	ActiveOrderNo      *string `gorm:"column:active_order_no;type:varchar(64)"`
	ProviderTradeState string  `gorm:"column:provider_trade_state;type:varchar(32)"`
	NotifyURL          string  `gorm:"column:notify_url;type:varchar(255)"`
	// PayURL TEXT：支付宝 RSA 跳转 URL 常超过 512。
	PayURL             string     `gorm:"column:pay_url;type:text"`
	ExpiresAt          *time.Time `gorm:"column:expires_at"`
	ProviderPaidAt     *time.Time `gorm:"column:provider_paid_at"`
	CreditedAt         *time.Time `gorm:"column:credited_at"`
	CallbackReceivedAt *time.Time `gorm:"column:callback_received_at"`
	NextQueryAt        *time.Time `gorm:"column:next_query_at;index:idx_pay_ord_status_nq_id_v2,priority:2"`
	QueryAttempts      int        `gorm:"column:query_attempts;not null;default:0"`
	QueryClaimToken    string     `gorm:"column:query_claim_token;type:varchar(64)"`
	QueryClaimUntil    *time.Time `gorm:"column:query_claim_until"`
	RecoveryClaimToken string     `gorm:"column:recovery_claim_token;type:varchar(64)"`
	RecoveryClaimUntil *time.Time `gorm:"column:recovery_claim_until"`
	NextRecoveryAt     *time.Time `gorm:"column:next_recovery_at"`
	LastErrorClass     string     `gorm:"column:last_error_class;type:varchar(64)"`
	LastNetworkStage   string     `gorm:"column:last_network_stage;type:varchar(32)"`
	CreateAttempts     int        `gorm:"column:create_attempts;not null;default:0"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;index:idx_pay_ord_status_upd_id_v2,priority:2"`
}

// TableName 固定表名。
func (orderRow) TableName() string { return "payment_orders" }

// Repo 是 payment.OrderRepo 的 GORM 实现，构建于 new-api 共享 *gorm.DB 之上。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

// 编译期断言：Repo 实现 payment.OrderRepo。
var _ payment.OrderRepo = (*Repo)(nil)

// New 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/迁 payment_orders 表（master 节点在 InitDB 后调用）。
func AutoMigrate(db *gorm.DB) error { return db.AutoMigrate(&orderRow{}) }

// Create 落库新订单；order_no / idempotency_key 唯一冲突 → payment.ErrOrderDuplicate。
func (r *Repo) Create(ctx context.Context, o *payment.PayOrder) error {
	row := toRow(o)
	if row.CreatedAt.IsZero() {
		row.CreatedAt = r.now()
	}
	row.UpdatedAt = row.CreatedAt
	err := r.db.WithContext(ctx).Create(row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isDuplicate(err) {
			return payment.ErrOrderDuplicate
		}
		return err
	}
	return nil
}

// SetPayURL 回填支付凭据；仅 credential 可写状态接受（防迟到 Prepay 写回 closed/replaced）。
func (r *Repo) SetPayURL(ctx context.Context, orderNo, payURL string) error {
	return r.SetPayURLFenced(ctx, orderNo, payURL, "", nil)
}

// SetPayURLFenced 带 create_state 白名单与可选 claim token 的条件更新。
func (r *Repo) SetPayURLFenced(ctx context.Context, orderNo, payURL, claimToken string, allowedStates []payment.CreateState) error {
	if len(allowedStates) == 0 {
		allowedStates = []payment.CreateState{
			payment.CreateStateLocalCreated,
			payment.CreateStatePrepayInflight,
			payment.CreateStatePrepayUnknown,
			payment.CreateStateCredentialReady,
		}
	}
	states := make([]string, len(allowedStates))
	for i, s := range allowedStates {
		states[i] = string(s)
	}
	q := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(payment.OrderCreated)).
		Where("create_state IN ?", states)
	if claimToken != "" {
		q = q.Where("query_claim_token = ? OR recovery_claim_token = ?", claimToken, claimToken)
	}
	res := q.Updates(map[string]any{
		"pay_url":      payURL,
		"create_state": string(payment.CreateStateCredentialReady),
		"updated_at":   r.now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return payment.ErrOrderNotFound
	}
	return nil
}

// GetByOrderNo 按订单号查；不存在 → payment.ErrOrderNotFound。
func (r *Repo) GetByOrderNo(ctx context.Context, orderNo string) (*payment.PayOrder, error) {
	var row orderRow
	if err := r.db.WithContext(ctx).Take(&row, "order_no = ?", orderNo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, payment.ErrOrderNotFound
		}
		return nil, err
	}
	return toPayOrder(&row), nil
}

// GetByIdempotencyKey 按幂等键查。
func (r *Repo) GetByIdempotencyKey(ctx context.Context, key string) (*payment.PayOrder, error) {
	if key == "" {
		return nil, payment.ErrOrderNotFound
	}
	var row orderRow
	if err := r.db.WithContext(ctx).Take(&row, "idempotency_key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, payment.ErrOrderNotFound
		}
		return nil, err
	}
	return toPayOrder(&row), nil
}

// GetByProviderTxn 按渠道+交易号查。
func (r *Repo) GetByProviderTxn(ctx context.Context, provider payment.Provider, txnID string) (*payment.PayOrder, error) {
	if txnID == "" {
		return nil, payment.ErrOrderNotFound
	}
	var row orderRow
	if err := r.db.WithContext(ctx).
		Take(&row, "provider = ? AND provider_transaction_id = ?", string(provider), txnID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, payment.ErrOrderNotFound
		}
		return nil, err
	}
	return toPayOrder(&row), nil
}

// CompareAndSetStatus 原子 CAS（条件 UPDATE）。
func (r *Repo) CompareAndSetStatus(ctx context.Context, orderNo string, from, to payment.OrderStatus) (bool, error) {
	updates := map[string]any{"status": string(to), "updated_at": r.now()}
	if to == payment.OrderCredited {
		updates["credited_at"] = r.now()
		updates["next_query_at"] = nil
	}
	if to == payment.OrderFailed {
		updates["next_query_at"] = nil
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(from)).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&orderRow{}).Where("order_no = ?", orderNo).Count(&cnt).Error; err != nil {
		return false, err
	}
	if cnt == 0 {
		return false, payment.ErrOrderNotFound
	}
	return false, nil
}

// ListByStatus 返回处于 status 且 updated_at 早于 before 的订单。
func (r *Repo) ListByStatus(ctx context.Context, status payment.OrderStatus, before time.Time, limit int) ([]*payment.PayOrder, error) {
	var rows []orderRow
	q := r.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", string(status), before).
		Order("updated_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToOrders(rows), nil
}

// ListDueForQuery 返回待主动查单的 created 订单。
func (r *Repo) ListDueForQuery(ctx context.Context, now time.Time, limit int) ([]*payment.PayOrder, error) {
	var rows []orderRow
	q := r.db.WithContext(ctx).
		Where("status = ? AND next_query_at IS NOT NULL AND next_query_at <= ?", string(payment.OrderCreated), now).
		Order("next_query_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToOrders(rows), nil
}

// ListPendingPrepay 待后台补 Prepay：local_created（含 legacy 空）+ pay_url 空 + 未过期 + 无有效 lease。
func (r *Repo) ListPendingPrepay(ctx context.Context, now time.Time, limit int) ([]*payment.PayOrder, error) {
	var rows []orderRow
	q := r.db.WithContext(ctx).
		Where("status = ?", string(payment.OrderCreated)).
		Where("create_state IN ?", []string{
			string(payment.CreateStateLocalCreated),
			"",
		}).
		Where("(pay_url IS NULL OR pay_url = '')").
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Where("(recovery_claim_until IS NULL OR recovery_claim_until <= ?)", now).
		Where("(query_claim_until IS NULL OR query_claim_until <= ?)", now).
		Order("created_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToOrders(rows), nil
}

// SavePaymentFacts 持久化支付机构事实。
// 冲突检测与真实复合唯一索引 (provider, provider_transaction_id) 语义一致。
func (r *Repo) SavePaymentFacts(ctx context.Context, orderNo string, facts payment.PaymentFacts) error {
	if facts.ProviderTransactionID != "" {
		// 先取本单 provider（facts.Provider 优先）
		var self orderRow
		if err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).Take(&self).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return payment.ErrOrderNotFound
			}
			return err
		}
		prov := string(facts.Provider)
		if prov == "" {
			prov = self.Provider
		}
		var other orderRow
		err := r.db.WithContext(ctx).
			Where("provider = ? AND provider_transaction_id = ? AND order_no <> ?",
				prov, facts.ProviderTransactionID, orderNo).
			Take(&other).Error
		if err == nil {
			return payment.ErrProviderTxnConflict
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	updates := map[string]any{"updated_at": r.now()}
	if facts.ProviderTransactionID != "" {
		updates["provider_transaction_id"] = facts.ProviderTransactionID
	}
	if !facts.ProviderPaidAt.IsZero() {
		updates["provider_paid_at"] = facts.ProviderPaidAt
	}
	if !facts.CallbackReceivedAt.IsZero() {
		// 仅首次写入：用 SQL 条件避免覆盖
		updates["callback_received_at"] = gorm.Expr("COALESCE(callback_received_at, ?)", facts.CallbackReceivedAt)
	}
	if facts.ClearNextQuery {
		updates["next_query_at"] = nil
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).Where("order_no = ?", orderNo).Updates(updates)
	if res.Error != nil {
		if isDuplicate(res.Error) || errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return payment.ErrProviderTxnConflict
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return payment.ErrOrderNotFound
	}
	return nil
}

// MarkCredited paid→credited + credited_at。
func (r *Repo) MarkCredited(ctx context.Context, orderNo string, at time.Time) (bool, error) {
	if at.IsZero() {
		at = r.now()
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(payment.OrderPaid)).
		Updates(map[string]any{
			"status":        string(payment.OrderCredited),
			"credited_at":   at,
			"next_query_at": nil,
			"updated_at":    r.now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&orderRow{}).Where("order_no = ?", orderNo).Count(&cnt).Error; err != nil {
		return false, err
	}
	if cnt == 0 {
		return false, payment.ErrOrderNotFound
	}
	return false, nil
}

// ScheduleNextQuery 更新查单调度字段。
func (r *Repo) ScheduleNextQuery(ctx context.Context, orderNo string, nextAt time.Time, attempts int) error {
	var next any
	if nextAt.IsZero() {
		next = nil
	} else {
		next = nextAt
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ?", orderNo).
		Updates(map[string]any{
			"next_query_at":  next,
			"query_attempts": attempts,
			"updated_at":     r.now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return payment.ErrOrderNotFound
	}
	return nil
}

// ClaimForQuery 认领 due 查单。
// 有效 Prepay operation lease（recovery_claim_until > now）时禁止 claim；
// lease 已过期的 prepay_inflight 允许 Query（崩溃后先查单，禁止盲重放 Prepay）。
func (r *Repo) ClaimForQuery(ctx context.Context, orderNo string, now, leaseUntil time.Time) (token string, ok bool, err error) {
	token = newClaimToken()
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(payment.OrderCreated)).
		Where("next_query_at IS NULL OR next_query_at <= ?", now).
		Where("query_claim_until IS NULL OR query_claim_until <= ?", now).
		Where("recovery_claim_until IS NULL OR recovery_claim_until <= ?", now).
		Updates(map[string]any{
			"query_claim_token": token,
			"query_claim_until": leaseUntil,
			"updated_at":        r.now(),
		})
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected != 1 {
		return "", false, nil
	}
	return token, true, nil
}

// ClaimForPrepay 唯一 Prepay 认领：仅 local_created（及 legacy 空）→ prepay_inflight。
// prepay_unknown 不得直接再 Prepay：崩溃/未知结果后必须先 Query；
// 仅当 Query 确认 ORDER_NOT_EXIST 后把状态重置为 local_created 才允许同 out_trade_no 再 Prepay。
func (r *Repo) ClaimForPrepay(ctx context.Context, orderNo string, now, leaseUntil time.Time) (token string, ok bool, err error) {
	token = newClaimToken()
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(payment.OrderCreated)).
		Where("create_state IN ?", []string{
			string(payment.CreateStateLocalCreated),
			"", // legacy empty
		}).
		Where("query_claim_until IS NULL OR query_claim_until <= ?", now).
		Where("recovery_claim_until IS NULL OR recovery_claim_until <= ?", now).
		Updates(map[string]any{
			"create_state":         string(payment.CreateStatePrepayInflight),
			"recovery_claim_token": token,
			"recovery_claim_until": leaseUntil,
			"next_query_at":        leaseUntil, // 覆盖 overall budget+grace，禁止 Prepay 完成前 Query 抢跑
			"updated_at":           r.now(),
		})
	if res.Error != nil {
		return "", false, res.Error
	}
	if res.RowsAffected != 1 {
		return "", false, nil
	}
	return token, true, nil
}

// FinishPrepayFenced Prepay 结果写回（token 必须匹配）。返回 applied。
func (r *Repo) FinishPrepayFenced(ctx context.Context, orderNo, token string, to payment.CreateState, payURL string, nextQueryAt time.Time, errorClass, stage string, createAttempts int) (bool, error) {
	updates := map[string]any{
		"recovery_claim_token": "",
		"recovery_claim_until": nil,
		"create_state":         string(to),
		"updated_at":           r.now(),
	}
	if payURL != "" {
		updates["pay_url"] = payURL
	}
	if !nextQueryAt.IsZero() {
		updates["next_query_at"] = nextQueryAt
	} else if to == payment.CreateStateDefinitiveReject {
		updates["next_query_at"] = nil
	}
	if errorClass != "" {
		updates["last_error_class"] = errorClass
		updates["last_network_stage"] = stage
		updates["create_attempts"] = createAttempts
	}
	if to == payment.CreateStateDefinitiveReject {
		updates["status"] = string(payment.OrderFailed)
		updates["next_query_at"] = nil
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ? AND recovery_claim_token = ?", orderNo, string(payment.OrderCreated), token).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// FinishQueryFenced 查单完成后的条件更新（必须 token 匹配且仍 created）。
func (r *Repo) FinishQueryFenced(ctx context.Context, orderNo, token string, nextAt time.Time, attempts int, createState payment.CreateState, tradeState string) (bool, error) {
	updates := map[string]any{
		"query_claim_token": "",
		"query_claim_until": nil,
		"query_attempts":    attempts,
		"updated_at":        r.now(),
	}
	if !nextAt.IsZero() {
		updates["next_query_at"] = nextAt
	} else {
		updates["next_query_at"] = nil
	}
	if createState != "" {
		updates["create_state"] = string(createState)
	}
	if tradeState != "" {
		updates["provider_trade_state"] = tradeState
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ? AND query_claim_token = ?", orderNo, string(payment.OrderCreated), token).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ReleaseQueryClaim 异常时条件释放 claim。
func (r *Repo) ReleaseQueryClaim(ctx context.Context, orderNo, token string) error {
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND query_claim_token = ?", orderNo, token).
		Updates(map[string]any{
			"query_claim_token": "",
			"query_claim_until": nil,
			"updated_at":        r.now(),
		})
	return res.Error
}

// TransitionCreateState CAS create_state（status 必须仍为 created）。
func (r *Repo) TransitionCreateState(ctx context.Context, orderNo string, from, to payment.CreateState) (bool, error) {
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ? AND create_state = ?", orderNo, string(payment.OrderCreated), string(from)).
		Updates(map[string]any{
			"create_state": string(to),
			"updated_at":   r.now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// TransitionCreateStateFenced 带 operation token。
func (r *Repo) TransitionCreateStateFenced(ctx context.Context, orderNo, token string, from, to payment.CreateState) (bool, error) {
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ? AND create_state = ? AND recovery_claim_token = ?",
			orderNo, string(payment.OrderCreated), string(from), token).
		Updates(map[string]any{
			"create_state": string(to),
			"updated_at":   r.now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ScheduleNextQueryFenced 带 token（query 或 prepay operation）的调度。
func (r *Repo) ScheduleNextQueryFenced(ctx context.Context, orderNo, token string, nextAt time.Time, attempts int) (bool, error) {
	var next any
	if nextAt.IsZero() {
		next = nil
	} else {
		next = nextAt
	}
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(payment.OrderCreated)).
		Where("query_claim_token = ? OR recovery_claim_token = ?", token, token).
		Updates(map[string]any{
			"next_query_at":  next,
			"query_attempts": attempts,
			"updated_at":     r.now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func newClaimToken() string {
	var b [16]byte
	_, _ = randRead(b[:])
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

// randRead 可测注入点。
var randRead = func(b []byte) (int, error) {
	return cryptoRandRead(b)
}

// RecordCreateFailure 记录创建失败观测字段。
func (r *Repo) RecordCreateFailure(ctx context.Context, orderNo string, errorClass, stage string, createAttempts int) error {
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ?", orderNo).
		Updates(map[string]any{
			"last_error_class":   errorClass,
			"last_network_stage": stage,
			"create_attempts":    createAttempts,
			"updated_at":         r.now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return payment.ErrOrderNotFound
	}
	return nil
}

func rowsToOrders(rows []orderRow) []*payment.PayOrder {
	out := make([]*payment.PayOrder, 0, len(rows))
	for i := range rows {
		out = append(out, toPayOrder(&rows[i]))
	}
	return out
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

func toRow(o *payment.PayOrder) *orderRow {
	cs := string(o.CreateState)
	if cs == "" {
		cs = string(payment.CreateStateLocalCreated)
	}
	root := o.RootOrderNo
	if root == "" {
		root = o.OrderNo
	}
	attempt := o.AttemptNo
	if attempt <= 0 {
		attempt = 1
	}
	active := o.ActiveOrderNo
	if active == "" {
		active = o.OrderNo
	}
	return &orderRow{
		OrderNo:               o.OrderNo,
		Type:                  string(o.Type),
		TenantID:              o.TenantID,
		UserID:                o.UserID,
		Provider:              string(o.Provider),
		AmountUSD:             o.AmountUSD,
		ActualPaid:            o.ActualPaid,
		ActualPaidFen:         o.ActualPaidFen,
		GroupID:               o.GroupID,
		PlanID:                o.PlanID,
		Subject:               o.Subject,
		Reference:             o.Reference,
		IdempotencyKey:        strPtr(o.IdempotencyKey),
		ProviderTransactionID: strPtr(o.ProviderTransactionID),
		Status:                string(o.Status),
		CreateState:           cs,
		RootOrderNo:           strPtr(root),
		ReplacesOrderNo:       strPtr(o.ReplacesOrderNo),
		AttemptNo:             attempt,
		ActiveOrderNo:         strPtr(active),
		ProviderTradeState:    o.ProviderTradeState,
		NotifyURL:             o.NotifyURL,
		PayURL:                o.PayURL,
		ExpiresAt:             timePtr(o.ExpiresAt),
		ProviderPaidAt:        timePtr(o.ProviderPaidAt),
		CreditedAt:            timePtr(o.CreditedAt),
		CallbackReceivedAt:    timePtr(o.CallbackReceivedAt),
		NextQueryAt:           timePtr(o.NextQueryAt),
		QueryAttempts:         o.QueryAttempts,
		QueryClaimToken:       o.QueryClaimToken,
		QueryClaimUntil:       timePtr(o.QueryClaimUntil),
		RecoveryClaimToken:    o.RecoveryClaimToken,
		RecoveryClaimUntil:    timePtr(o.RecoveryClaimUntil),
		NextRecoveryAt:        timePtr(o.NextRecoveryAt),
		LastErrorClass:        o.LastErrorClass,
		LastNetworkStage:      o.LastNetworkStage,
		CreateAttempts:        o.CreateAttempts,
		CreatedAt:             o.CreatedAt,
		UpdatedAt:             o.UpdatedAt,
	}
}

func toPayOrder(row *orderRow) *payment.PayOrder {
	cs := payment.CreateState(row.CreateState)
	if cs == "" {
		cs = payment.CreateStateLocalCreated
	}
	root := derefStr(row.RootOrderNo)
	if root == "" {
		root = row.OrderNo
	}
	active := derefStr(row.ActiveOrderNo)
	if active == "" {
		active = row.OrderNo
	}
	return &payment.PayOrder{
		OrderNo:               row.OrderNo,
		Type:                  payment.OrderType(row.Type),
		TenantID:              row.TenantID,
		UserID:                row.UserID,
		Provider:              payment.Provider(row.Provider),
		AmountUSD:             row.AmountUSD,
		ActualPaid:            row.ActualPaid,
		ActualPaidFen:         row.ActualPaidFen,
		GroupID:               row.GroupID,
		PlanID:                row.PlanID,
		Subject:               row.Subject,
		Reference:             row.Reference,
		IdempotencyKey:        derefStr(row.IdempotencyKey),
		ProviderTransactionID: derefStr(row.ProviderTransactionID),
		Status:                payment.OrderStatus(row.Status),
		CreateState:           cs,
		RootOrderNo:           root,
		ReplacesOrderNo:       derefStr(row.ReplacesOrderNo),
		AttemptNo:             row.AttemptNo,
		ActiveOrderNo:         active,
		ProviderTradeState:    row.ProviderTradeState,
		NotifyURL:             row.NotifyURL,
		PayURL:                row.PayURL,
		ExpiresAt:             derefTime(row.ExpiresAt),
		ProviderPaidAt:        derefTime(row.ProviderPaidAt),
		CreditedAt:            derefTime(row.CreditedAt),
		CallbackReceivedAt:    derefTime(row.CallbackReceivedAt),
		NextQueryAt:           derefTime(row.NextQueryAt),
		QueryAttempts:         row.QueryAttempts,
		QueryClaimToken:       row.QueryClaimToken,
		QueryClaimUntil:       derefTime(row.QueryClaimUntil),
		RecoveryClaimToken:    row.RecoveryClaimToken,
		RecoveryClaimUntil:    derefTime(row.RecoveryClaimUntil),
		NextRecoveryAt:        derefTime(row.NextRecoveryAt),
		LastErrorClass:        row.LastErrorClass,
		LastNetworkStage:      row.LastNetworkStage,
		CreateAttempts:        row.CreateAttempts,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, frag := range []string{"duplicate entry", "unique constraint", "duplicate key", "1062"} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}
