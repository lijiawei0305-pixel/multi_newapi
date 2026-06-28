// Package gormrepo 用真实 GORM 实现 payment.OrderRepo（支付订单持久化）。
//
// 单表 payment_orders（detailed-design §6.2）：
//   - order_no 唯一索引 —— 下单幂等键 + 重复回调定位键；
//   - status 状态机（created→paid→credited / failed）用**条件 UPDATE**做原子 CAS：
//     `UPDATE ... WHERE order_no=? AND status=from`，RowsAffected==1 即本次为推进者。
//     这与 SELECT ... FOR UPDATE + UPDATE 给出**同等强幂等**（DB 层串行化该行写），
//     但可移植到 sqlite（单测）与 MySQL（生产），无需显式行锁。
//
// 金额列：USD 入账额度用 decimal(20,8)（与 wallet/tokenplan 一致）；实付 ¥ 用 decimal(20,2)。
package gormrepo

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// orderRow 是 payment_orders 表的 GORM 模型。
type orderRow struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo    string    `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:idx_payment_orders_no"`
	Type       string    `gorm:"column:type;type:varchar(16);not null;index:idx_payment_orders_type"`
	TenantID   int64     `gorm:"column:tenant_id;not null;index:idx_payment_orders_tenant_user,priority:1"`
	UserID     int64     `gorm:"column:user_id;not null;index:idx_payment_orders_tenant_user,priority:2"`
	Provider   string    `gorm:"column:provider;type:varchar(16);not null"`
	AmountUSD  float64   `gorm:"column:amount_usd;type:decimal(20,8);not null;default:0"`
	ActualPaid float64   `gorm:"column:actual_paid;type:decimal(20,2);not null;default:0"`
	GroupID    int64     `gorm:"column:group_id;not null;default:0"`
	PlanID     int64     `gorm:"column:plan_id;not null;default:0"`
	Subject    string    `gorm:"column:subject;type:varchar(255)"`
	Reference  string    `gorm:"column:reference;type:varchar(255)"`
	Status     string    `gorm:"column:status;type:varchar(16);not null;index:idx_payment_orders_status"`
	NotifyURL  string    `gorm:"column:notify_url;type:varchar(255)"`
	PayURL     string    `gorm:"column:pay_url;type:varchar(512)"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
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

// Create 落库新订单；order_no 唯一冲突 → payment.ErrOrderDuplicate。
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

// CompareAndSetStatus 原子 CAS（条件 UPDATE）：仅当当前 status==from 才置为 to。
//
//	ok=true  本次 RowsAffected==1，为状态推进者；
//	ok=false 0 行受影响 —— 订单存在但 status!=from（已被并发推进 / 已终态）；
//	订单不存在 → payment.ErrOrderNotFound。
func (r *Repo) CompareAndSetStatus(ctx context.Context, orderNo string, from, to payment.OrderStatus) (bool, error) {
	res := r.db.WithContext(ctx).Model(&orderRow{}).
		Where("order_no = ? AND status = ?", orderNo, string(from)).
		Updates(map[string]any{"status": string(to), "updated_at": r.now()})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	// 0 行：区分「订单不存在」与「状态非 from」。
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&orderRow{}).Where("order_no = ?", orderNo).Count(&cnt).Error; err != nil {
		return false, err
	}
	if cnt == 0 {
		return false, payment.ErrOrderNotFound
	}
	return false, nil
}

// toRow 把领域订单映射为表行。
func toRow(o *payment.PayOrder) *orderRow {
	return &orderRow{
		OrderNo:    o.OrderNo,
		Type:       string(o.Type),
		TenantID:   o.TenantID,
		UserID:     o.UserID,
		Provider:   string(o.Provider),
		AmountUSD:  o.AmountUSD,
		ActualPaid: o.ActualPaid,
		GroupID:    o.GroupID,
		PlanID:     o.PlanID,
		Subject:    o.Subject,
		Reference:  o.Reference,
		Status:     string(o.Status),
		NotifyURL:  o.NotifyURL,
		PayURL:     o.PayURL,
		CreatedAt:  o.CreatedAt,
		UpdatedAt:  o.UpdatedAt,
	}
}

// toPayOrder 把表行映射回领域订单。
func toPayOrder(row *orderRow) *payment.PayOrder {
	return &payment.PayOrder{
		OrderNo:    row.OrderNo,
		Type:       payment.OrderType(row.Type),
		TenantID:   row.TenantID,
		UserID:     row.UserID,
		Provider:   payment.Provider(row.Provider),
		AmountUSD:  row.AmountUSD,
		ActualPaid: row.ActualPaid,
		GroupID:    row.GroupID,
		PlanID:     row.PlanID,
		Subject:    row.Subject,
		Reference:  row.Reference,
		Status:     payment.OrderStatus(row.Status),
		NotifyURL:  row.NotifyURL,
		PayURL:     row.PayURL,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

// isDuplicate 兜底识别唯一约束冲突（当 gorm.TranslateError 未开启时，按驱动错误串匹配）。
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
