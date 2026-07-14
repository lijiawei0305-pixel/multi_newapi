package mtwire

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/model"
)

// ============================================================================
// Phase 2 · 目标③ —— 把 tokenplan 套餐桥接成 new-api 原生订阅
//
// 架构：复用 new-api 原生计费。激活时建一条原生 model.UserSubscription（额度桶），原生 /v1 在用户
// 有 active 订阅时自动按订阅桶计量（含预扣/流式/退款）；我们仅在套餐购买/激活两端做桥接，不重写计费、
// 不碰 relay。BillingPreference 默认即 "subscription_first"（common.NormalizeBillingPreference 兜底，
// 见 service/billing_session.go），故只要存在 active 原生订阅，/v1 即自动走订阅桶，无需额外设置偏好。
//
// 关键映射（tokenplan → 原生）：
//   - month_limit_usd × QuotaPerUnit → UserSubscription.AmountTotal（订阅额度上限，$1=500k quota）
//   - valid_days                     → 周期（DurationUnit=day → EndTime = now + valid_days）
//   - x1 不叠分组                     → 原生 plan.UpgradeGroup 留空（不升用户组、不改倍率）
//   - 硬封顶（用尽即止）              → AllowWalletOverflow=false（对齐 tokenplan exhausted 语义）
//
// 注意原生 model.UserSubscription 独占物理表 user_subscriptions；我们的 tokenplan 订阅台账已改名为
// tokenplan_subscriptions 避让（见 internal/tokenplan/gormrepo）。本文件另建两张 mt_ 前缀表，均不与
// 原生表冲突。
// ============================================================================

// SubscriptionOrderPrefix 是 tokenplan 套餐订单号前缀。支付回调据此把订单分发到
// ActivatePaidTokenplanOrder（与 Track 2 钱包充值订单前缀 RCG 分开存、分开分发）。
const SubscriptionOrderPrefix = "SUB"

// 订单激活状态机（最简两态）。激活 = 建原生订阅 + 落我们订阅记录/分润。
const (
	subOrderPending   = "pending"
	subOrderActivated = "activated"
	// subOrderExpired 终态：pending 单下单超时仍未付 / 网关查无此单（永不会被支付），由对账过期兜底置此。
	// 非 pending/activated → 不再被对账扫描（见 ReconcileStuckSubscriptions 的 WHERE）。
	subOrderExpired = "expired"
)

// nativeSubSource 写入原生 UserSubscription.Source，标记该订阅由 tokenplan 桥接而来（审计/排障用）。
const nativeSubSource = "tokenplan"

// IsSubscriptionOrderNo 报告订单号是否为 tokenplan 套餐订单（SUB 前缀）。
// 供 Track 2/Master 的支付回调按前缀分发：SUB→ActivatePaidTokenplanOrder，RCG→钱包充值。
func IsSubscriptionOrderNo(orderNo string) bool {
	return strings.HasPrefix(orderNo, SubscriptionOrderPrefix)
}

// ---- 表 1：mt_subscription_orders —— tokenplan 套餐待支付订单 + 激活状态机（与 RCG 充值订单分开存） ----

type subscriptionOrderRow struct {
	OrderNo      string    `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	TenantID     int64     `gorm:"column:tenant_id;not null;index"`
	UserID       int64     `gorm:"column:user_id;not null;index"`
	AmountCNY    float64   `gorm:"column:amount_cny;type:decimal(20,2);not null;default:0"`
	Provider     string    `gorm:"column:provider;type:varchar(16);not null;default:''"` // wxpay|alipay：下单时回填，供真实回调路由 + 主动查单识别渠道
	Status       string    `gorm:"column:status;type:varchar(16);not null;default:pending;index"`
	NativePlanID int64     `gorm:"column:native_plan_id;not null;default:0"`    // 激活时回填：原生 SubscriptionPlan.id
	NativeSubID  int64     `gorm:"column:native_sub_id;not null;default:0"`     // 激活时回填：原生 UserSubscription.id
	Settled      bool      `gorm:"column:settled;not null;default:false;index"` // 步骤③（订阅记录+代理分润）已落；对账据此补驱动「已激活未结算」卡单（M1）
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名（mt_ 前缀，避让原生 subscription_orders）。
func (subscriptionOrderRow) TableName() string { return "mt_subscription_orders" }

// ---- 表 2：mt_native_subscription_plans —— tokenplan 套餐 → 原生 SubscriptionPlan 映射 ----
//
// 一个 tokenplan 套餐对应一条稳定的原生 plan（reset=never、不升组）。原生 PreConsumeUserSubscription
// 按 sub.PlanId 反查该 plan（须长期存在），故用映射表保证「按套餐去重、只建一条」。

type nativePlanMapRow struct {
	TokenPlanID  int64     `gorm:"column:token_plan_id;primaryKey"` // tokenplan token_plans.id
	NativePlanID int64     `gorm:"column:native_plan_id;not null"`  // 原生 subscription_plans.id
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName 固定表名（mt_ 前缀，避让原生 subscription_plans）。
func (nativePlanMapRow) TableName() string { return "mt_native_subscription_plans" }

// migrateSubscriptionBridge 建桥接两张表（由 App.Migrate 调用）。
func migrateSubscriptionBridge(db *gorm.DB) error {
	return db.AutoMigrate(&subscriptionOrderRow{}, &nativePlanMapRow{})
}

// ---- 订单存储（最简 OrderRepo 思路：唯一插入；状态迁移在激活事务内做条件 UPDATE/CAS） ----

type subOrderStore struct{ db *gorm.DB }

func newSubOrderStore(db *gorm.DB) *subOrderStore { return &subOrderStore{db: db} }

// create 落一条新订单；order_no 主键冲突即唯一约束冲突（由调用方按需处理）。
func (s *subOrderStore) create(ctx context.Context, row *subscriptionOrderRow) error {
	return s.db.WithContext(ctx).Create(row).Error
}

// setProvider 回填订单支付渠道（下单选定 wxpay/alipay 后调用；仅在仍为 pending 时更新，幂等）。
// 与 tokenplan 包解耦：订单由 subPayment.CreateOrder 落库（不含渠道），此处由 mtwire 装配层补写。
func (s *subOrderStore) setProvider(ctx context.Context, orderNo, provider string) error {
	if orderNo == "" || provider == "" {
		return nil
	}
	return s.db.WithContext(ctx).Model(&subscriptionOrderRow{}).
		Where("order_no = ? AND status = ?", orderNo, subOrderPending).
		Update("provider", provider).Error
}

// ---- subPayment：tokenplan.PaymentGateway 实现，取代 wire.go 的 stubPayment ----
//
// 下单 = 落一条真实 pending 订单（前缀 SUB），可被支付回调用 ActivatePaidTokenplanOrder 激活。
// 本网关只持久化订单意图 + 返回占位支付页 URL；真实支付凭据由 HTTP 装配层
// （mtwire.HandlePurchase → subscriptionPayURL → providerManager.CreatePay 进程内向平台下单）按
// order_no 取回并覆盖（与 RCG 充值同形），故下方 PayURL 仅作 providerMgr 未装配时的回退占位。
type subPayment struct{ orders *subOrderStore }

func newSubPayment(orders *subOrderStore) *subPayment { return &subPayment{orders: orders} }

var _ tokenplan.PaymentGateway = (*subPayment)(nil)

// CreateOrder 生成 SUB 订单号、落一条 pending 订单，返回支付凭据（占位支付页）。
// 套餐快照（month_limit/valid_days/agent_cost）由 Purchase 流程随后 SavePendingPurchase 落到
// pending_subscription_orders（同 order_no），激活时据此还原。
func (p *subPayment) CreateOrder(ctx context.Context, in tokenplan.OrderInput) (*tokenplan.PayOrder, error) {
	orderNo := SubscriptionOrderPrefix + strings.ToUpper(randToken(12))
	now := time.Now()
	row := &subscriptionOrderRow{
		OrderNo:   orderNo,
		TenantID:  in.TenantID,
		UserID:    in.UserID,
		AmountCNY: in.AmountCNY,
		Status:    subOrderPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := p.orders.create(ctx, row); err != nil {
		return nil, err
	}
	return &tokenplan.PayOrder{
		OrderID: orderNo,
		PayURL:  "/console/tokenplan/pay?order=" + orderNo,
	}, nil
}

// ---- 原生订阅激活（核心桥接） ----

// usdToQuota 把订阅 USD 额度换算成原生 quota（$1 = common.QuotaPerUnit）。向上取整，对齐原生
// calcSubscriptionBalanceQuota 的 decimal.Ceil。换算走统一核心 usdToQuotaRound（见 money.go）。
func usdToQuota(usd float64) int64 {
	return usdToQuotaRound(usd, roundUp)
}

// buildNativeBridgePlan 构造创建原生订阅用的「内存态」SubscriptionPlan（不落库，仅供
// CreateUserSubscriptionFromPlanTx 读取并快照进 UserSubscription，从而精确还原购买快照）：
//   - Id：已持久化的原生 plan id（创建函数要求非零 id 作 FK / 反查锚点）；
//   - TotalAmount：month_limit_usd × QuotaPerUnit（订阅额度上限）；
//   - 周期：DurationUnit=day、DurationValue=valid_days；
//   - QuotaResetPeriod=never：整窗口一笔预算，不做月内重置；
//   - AllowWalletOverflow=false：用尽即止（硬封顶，对齐 tokenplan exhausted 语义）；
//   - UpgradeGroup/DowngradeGroup 留空：不改用户分组（x1，不叠分组倍率）。
func buildNativeBridgePlan(nativePlanID int64, snap *tokenplan.PendingPurchase) *model.SubscriptionPlan {
	return &model.SubscriptionPlan{
		Id:                  int(nativePlanID),
		TotalAmount:         usdToQuota(snap.MonthLimitUSD),
		DurationUnit:        model.SubscriptionDurationDay,
		DurationValue:       snap.ValidDays,
		QuotaResetPeriod:    model.SubscriptionResetNever,
		AllowWalletOverflow: common.GetPointer(false),
		MaxPurchasePerUser:  0, // 限购由我们的 RiskEngine 把关，不用原生
	}
}

// nativePlanTitle 给原生 plan 一个可读标题（便于在原生订阅后台辨认来源）。
func (a *App) nativePlanTitle(ctx context.Context, planID int64) string {
	if p, err := a.TokenPlanRepo.GetPlan(ctx, planID); err == nil && p != nil {
		return "[tokenplan] " + p.Name
	}
	return "[tokenplan] #" + strconv.FormatInt(planID, 10)
}

// ensureNativeSubscriptionPlan 幂等取得某 tokenplan 套餐对应的稳定原生 SubscriptionPlan id：
// 命中 mt_native_subscription_plans 映射直接返回；否则建一条原生 plan（reset=never、不升组、
// 不允许余额购买）并落映射（OnConflict 去重，应对并发首建）。在传入 tx 内完成。
func (a *App) ensureNativeSubscriptionPlan(ctx context.Context, tx *gorm.DB, snap *tokenplan.PendingPurchase) (int64, error) {
	var m nativePlanMapRow
	err := tx.Take(&m, "token_plan_id = ?", snap.PlanID).Error
	if err == nil {
		return m.NativePlanID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	plan := &model.SubscriptionPlan{
		Title:               a.nativePlanTitle(ctx, snap.PlanID),
		Currency:            "USD",
		PriceAmount:         0, // 不走原生余额购买；定价/收益在 tokenplan 侧
		DurationUnit:        model.SubscriptionDurationDay,
		DurationValue:       snap.ValidDays,
		Enabled:             true,
		AllowBalancePay:     common.GetPointer(false),
		AllowWalletOverflow: common.GetPointer(false),
		TotalAmount:         usdToQuota(snap.MonthLimitUSD),
		QuotaResetPeriod:    model.SubscriptionResetNever,
	}
	if err := tx.Create(plan).Error; err != nil {
		return 0, err
	}
	m = nativePlanMapRow{TokenPlanID: snap.PlanID, NativePlanID: int64(plan.Id), CreatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&m).Error; err != nil {
		return 0, err
	}
	// 并发首建去重：回读最终生效的映射（败者的孤儿 plan 无害、reset=never 不被使用）。
	if err := tx.Take(&m, "token_plan_id = ?", snap.PlanID).Error; err != nil {
		return 0, err
	}
	return m.NativePlanID, nil
}

// activateNativeSubHook 默认指向 App.defaultActivateNativeSub；单测替换为计数桩以验证
// 「同 orderNo 重复调用只激活一次原生订阅」。用包级 seam（而非 App 字段）以最小化对共享 wire.go 的改动。
var activateNativeSubHook = func(a *App, ctx context.Context, tx *gorm.DB, snap *tokenplan.PendingPurchase) (int64, int64, error) {
	return a.defaultActivateNativeSub(ctx, tx, snap)
}

// defaultActivateNativeSub 取/建原生 plan，再用原生 CreateUserSubscriptionFromPlanTx 建一条 active
// 原生订阅（复用原生创建函数，不直插表）。返回 (nativeSubID, nativePlanID)。
func (a *App) defaultActivateNativeSub(ctx context.Context, tx *gorm.DB, snap *tokenplan.PendingPurchase) (int64, int64, error) {
	nativePlanID, err := a.ensureNativeSubscriptionPlan(ctx, tx, snap)
	if err != nil {
		return 0, 0, err
	}
	plan := buildNativeBridgePlan(nativePlanID, snap)
	sub, err := model.CreateUserSubscriptionFromPlanTx(tx, int(snap.UserID), plan, nativeSubSource)
	if err != nil {
		return 0, nativePlanID, err
	}
	return int64(sub.Id), nativePlanID, nil
}

// ActivatePaidTokenplanOrder 在支付成功后激活一笔 tokenplan 套餐订单（供 Track 2/Master 的支付回调
// 按 SUB 前缀分发调用，签名供 OrderSink/回调直接接线）：
//
//	① 取购买快照（pending_subscription_orders，Purchase 时已落）；
//	② 幂等激活一条原生 UserSubscription（month_limit_usd×QuotaPerUnit 为额度上限、valid_days 为周期、
//	   x1 不升组）——此后原生 /v1 自动走订阅桶计量（BillingPreference 默认 subscription_first）；
//	③ 调 tokenplan.ActivateFromPayment 落我们订阅记录 + 首次发代理差价分润。
//
// 幂等（同 orderNo 重复调用只激活一次）：
//   - 原生订阅：订单行 pending→activated 的条件 UPDATE（CAS）守门，只有抢到迁移的调用方建原生订阅，
//     且与建订阅同事务（失败回滚→订单退回 pending 可重试，不留半成品）；
//   - 我们记录/分润：ActivateFromPayment 自身按 source_order_id / (SourceType,SourceID) 幂等，
//     即便重复调用（如步骤②已 activated 但②③之间崩溃后重试）也只落一次、只入账一次。
//
// paidAmountCNY 是支付平台回传的用户实付（元），用于反篡改一致性校验（与库内 AmountCNY 比对）；
// <=0 表示调用方未提供（如对账兜底主动查单）——跳过比对。激活额度/周期一律以购买快照为准。
func (a *App) ActivatePaidTokenplanOrder(ctx context.Context, orderNo string, paidAmountCNY float64) error {
	if !IsSubscriptionOrderNo(orderNo) {
		return tokenplan.ErrSubscriptionNotFound // 非 SUB 订单不归本入口
	}
	snap, err := a.TokenPlanRepo.GetPendingPurchase(ctx, orderNo)
	if err != nil {
		return err // ErrSubscriptionNotFound 等
	}

	// 步骤②：原生订阅激活（CAS 守门 + 同事务建原生订阅）。
	if err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ord subscriptionOrderRow
		if e := tx.Take(&ord, "order_no = ?", orderNo).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return tokenplan.ErrSubscriptionNotFound
			}
			return e
		}
		if ord.Status == subOrderActivated {
			return nil // 已激活：原生订阅不再重复建（步骤③仍会幂等补齐我们的记录）
		}
		if ord.Status != subOrderPending {
			return tokenplan.ErrSubscriptionNotFound
		}
		// 反篡改：回传实付金额必须与库内订单一致（仅对未激活单校验；激活额度以快照为准）。
		if !amountMatchesCNY(paidAmountCNY, ord.AmountCNY) {
			return payment.ErrAmountMismatch
		}
		// CAS：pending→activated，抢到者负责建原生订阅。
		res := tx.Model(&subscriptionOrderRow{}).
			Where("order_no = ? AND status = ?", orderNo, subOrderPending).
			Updates(map[string]any{"status": subOrderActivated, "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 并发败者：回读，若已 activated 即幂等成功。
			if e := tx.Take(&ord, "order_no = ?", orderNo).Error; e != nil {
				return e
			}
			if ord.Status == subOrderActivated {
				return nil
			}
			return tokenplan.ErrSubscriptionNotFound
		}
		nativeSubID, nativePlanID, e := activateNativeSubHook(a, ctx, tx, snap)
		if e != nil {
			return e // 回滚 → 订单退回 pending，可重试
		}
		return tx.Model(&subscriptionOrderRow{}).Where("order_no = ?", orderNo).
			Updates(map[string]any{
				"native_sub_id":  nativeSubID,
				"native_plan_id": nativePlanID,
				"updated_at":     time.Now(),
			}).Error
	}); err != nil {
		return err
	}

	// 步骤③：我们订阅记录 + 代理差价分润（幂等）。此步在步骤②事务**之外**——若失败，订单已 activated
	// （用户已可用），故不回滚步骤②；改由 settled 标记 + 对账补驱动兜底（M1），避免代理分润/订阅记录永久遗漏。
	if _, err := a.Subscriptions.ActivateFromPayment(ctx, orderNo); err != nil {
		return err
	}
	// 步骤③完成 → 置 settled，供 ReconcileStuckSubscriptions 区分「已激活但③未落」的卡单。
	// 置位失败不返错（与 credit.go 末次 CAS 同范式）：分润/记录已幂等落账，仅缺 settled 标记，
	// 留 settled=false 由 ReconcileStuckSubscriptions 幂等补驱动，避免多余的回调重推。
	if err := a.DB.WithContext(ctx).Model(&subscriptionOrderRow{}).
		Where("order_no = ?", orderNo).Update("settled", true).Error; err != nil {
		common.SysLog("activate sub: set settled failed (order " + orderNo + "): " + err.Error())
	}
	return nil
}
