package mtwire

// 充值 / 支付装配（Phase 2 · 目标③，支付重构后）。
//
// 架构：充值入 new-api **原生 quota**（$1 = QuotaPerUnit，IncreaseUserQuota）。微信/支付宝下单经
// 进程内 providerManager（inProcessPaySDK）直连真实平台，回调直达主站 /api/pay/*/notify（见
// payment_inprocess.go）。本文件是主站侧装配：
//   - HandleWalletRecharge 下单：校验 → 算实付¥ → 经 RechargeGateway 落库 RCG 订单 + 进程内下单 → 返支付凭据；
//   - rechargeQuotaSink     入账：RCG 订单 → 原生 quota（$1 = QuotaPerUnit）。
//
// 金额可信：以**库内订单金额**入账，绝不信回调报文金额（回调仅作反篡改校验，见 payment.CreditPaidOrder）。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// minRechargeUSD 充值美元最低额（对齐 operation_setting.MinTopUp 口径、UI $1 下限）。
const minRechargeUSD = 1.0

// 充值/支付相关错误码（沿用模块前缀约定）。
var (
	errRechargeAmountTooSmall  = apperr.New("RECHARGE_AMOUNT_TOO_SMALL", fmt.Sprintf("充值金额最低 $%g", minRechargeUSD), http.StatusBadRequest)
	errRechargeUnauthenticated = apperr.New("RECHARGE_UNAUTHENTICATED", "登录态缺失", http.StatusUnauthorized)
	errRechargeOrderNoRequired = apperr.New("RECHARGE_ORDER_NO_REQUIRED", "缺少订单号", http.StatusBadRequest)
)

// rechargeConfig 是主站侧充值装配参数。
type rechargeConfig struct {
	notifyBaseURL string // 异步回调公网基址（回填订单 notify_url）；缺省取 system_setting.ServerAddress
}

// loadRechargeConfig 读取充值装配参数（notify 基址优先 MT_PAY_NOTIFY_BASE，缺省 system_setting.ServerAddress）。
func loadRechargeConfig() rechargeConfig {
	return rechargeConfig{
		notifyBaseURL: resolveNotifyBase(),
	}
}

// ---- 入账分发目标（OrderSink 实现）----

// rechargeCreditLedgerRow 是充值入账幂等台账（每 order_no 至多一条）。它把「是否已入账」
// 从订单状态机中解耦出来，作为**唯一事实源**：无论 OnPaid 被调用几次（回调重推、对账
// ReconcileStuckPaid 重跑、崩溃恢复），台账写入与额度自增在同一 DB 事务内完成，order_no
// 唯一约束保证每单只入账一次——彻底消除额度双扣（审计 C1），并顺带补上充值入账的审计台账。
type rechargeCreditLedgerRow struct {
	OrderNo   string    `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	TenantID  int64     `gorm:"column:tenant_id;not null;index"`
	UserID    int64     `gorm:"column:user_id;not null;index"`
	Quota     int64     `gorm:"column:quota;not null"`                              // 入账的原生 quota 单位
	AmountUSD float64   `gorm:"column:amount_usd;type:decimal(20,4);not null;default:0"` // 入账美元额（审计）
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名（mt_ 前缀，避让原生表）。
func (rechargeCreditLedgerRow) TableName() string { return "mt_recharge_credit_ledger" }

// migrateRechargeLedger 建充值入账幂等台账表（由 App.Migrate 调用）。
func migrateRechargeLedger(db *gorm.DB) error {
	return db.AutoMigrate(&rechargeCreditLedgerRow{})
}

// rechargeQuotaSink 把充值订单入账到 new-api 原生 quota（$1 = QuotaPerUnit）。
// **强幂等**：以 mt_recharge_credit_ledger(order_no UNIQUE) 为幂等键，台账写入与额度自增同事务，
// 重复调用（回调重推 / 对账重跑 / 崩溃恢复）只入账一次，不双扣（审计 C1 修复）。
type rechargeQuotaSink struct{ db *gorm.DB }

func (s rechargeQuotaSink) OnPaid(ctx context.Context, o payment.PaidOrder) error {
	q := rechargeQuota(o.AmountUSD)
	if q <= 0 {
		return nil
	}
	// TODO(recharge_spread / 口径未决)：充值差价分润 = 用户实付¥ − 代理成本¥。当前单一汇率模型下，
	// 用户实付 = AmountUSD × USDExchangeRate（= 主站标准价），代理无独立「充值成本/加价率」字段，
	// 故差价恒为 0、暂不入账。待数据模型补充 agent 充值加价/成本率后，在此按
	// (o.ActualPaid − agentRechargeCostCNY) 经 AgentEarnings.AddEarning(source=recharge_spread,
	// SourceID=o.OrderNo) 幂等落账（须先有 agent_profile）。详见报告「风险/未决」。
	credited := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 幂等台账：order_no 为主键。已存在（本单已入账）→ 唯一约束冲突 → 短路不加额度。
		// 用「唯一约束错误」判定幂等，而非 RowsAffected 数值——后者依赖 driver 的 affected/found-rows
		// 语义（如 DSN 开 clientFoundRows 会使 OnConflict 的 RowsAffected 失真而误判成双扣，审计复核 M-1）。
		if err := tx.Create(&rechargeCreditLedgerRow{
			OrderNo:   o.OrderNo,
			TenantID:  o.TenantID,
			UserID:    o.UserID,
			Quota:     int64(q),
			AmountUSD: o.AmountUSD,
			CreatedAt: time.Now(),
		}).Error; err != nil {
			if isDuplicateLedgerErr(err) {
				return nil // 已入账过 → 幂等短路，不重复加额度
			}
			return err
		}
		credited = true
		// 与台账写入同事务落 users.quota，二者原子：要么都成、要么都回滚。
		return tx.Model(&model.User{}).Where("id = ?", o.UserID).
			Update("quota", gorm.Expr("quota + ?", q)).Error
	}); err != nil {
		return err
	}
	if credited {
		// DB 已提交。使额度缓存失效（下次读从 DB 重载，缓存永不与 DB 发散——顺带修审计 M5）。
		if cErr := model.InvalidateUserCache(int(o.UserID)); cErr != nil {
			common.SysLog("recharge credit: invalidate user cache failed (order " + o.OrderNo + "): " + cErr.Error())
		}
	}
	return nil
}

// isDuplicateLedgerErr 报告是否为唯一约束/主键冲突错误（跨 MySQL/sqlite，driver 无关）。
// 与 internal/payment/gormrepo.Create 同范式：既认 gorm 翻译错误（TranslateError 开启时，如测试库），
// 又认原始 driver 错误串（主库未开 TranslateError）——两路兜底，稳过。
func isDuplicateLedgerErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, frag := range []string{"duplicate entry", "unique constraint", "duplicate key", "duplicated key", "1062"} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

// rechargeQuota 把充值美元额折算为 new-api 内部 quota 单位（$1 = common.QuotaPerUnit）。
func rechargeQuota(amountUSD float64) int {
	if amountUSD <= 0 {
		return 0
	}
	return int(amountUSD * common.QuotaPerUnit)
}

// actualPaidCNY 计算用户实付人民币 = 美元额 × 汇率（收益币种；差价基准）。
func actualPaidCNY(amountUSD, rate float64) float64 {
	return amountUSD * rate
}

// amountToleranceCNY 金额比对容差（元）：≤1 分视为相等，吸收浮点/汇率取整噪声。
const amountToleranceCNY = 0.011

// amountMatchesCNY 报告回调实付金额是否与库内订单金额一致（反篡改）。
// paidCNY<=0 视为「调用方未提供」（如对账兜底主动查单），跳过比对。
func amountMatchesCNY(paidCNY, orderCNY float64) bool {
	if paidCNY <= 0 {
		return true
	}
	return math.Abs(paidCNY-orderCNY) <= amountToleranceCNY
}

// 说明：tokenplan 套餐订单（SUB 前缀）的激活由 App.ActivatePaidTokenplanOrder
// （internal/mtwire/subscription_bridge.go）提供，回调按前缀分发调用（见 payment_inprocess.go）。
// SUB 订单存于 mt_subscription_orders，不在本模块 payment_orders（RCG 充值订单）中。

// ---- HTTP 处理器 ----

// rechargeRequest 是 POST /api/tenant/wallet/recharge 入参。
type rechargeRequest struct {
	AmountUSD float64 `json:"amount_usd"`
	Provider  string  `json:"provider"` // wxpay | alipay
}

// HandleWalletRecharge POST /api/tenant/wallet/recharge —— 钱包充值下单。需 UserAuth + Host 租户。
//
// 流程：校验 amount_usd≥1 与渠道 → 实付¥=usd×汇率 → RechargeGateway.CreateOrder（落库 RCG 订单 +
// 进程内向平台下单拿支付凭据）→ 返回 {order_no, amount_*, provider, pay:{wxpay_qr|alipay_url}}。
func (a *App) HandleWalletRecharge(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, errRechargeUnauthenticated)
		return
	}
	var body rechargeRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	provider := payment.Provider(body.Provider)
	if !provider.Valid() {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	if body.AmountUSD < minRechargeUSD {
		respondErr(c, errRechargeAmountTooSmall)
		return
	}
	// 渠道必须可用（enabled 且凭据齐全，进程内判断）方可下单。
	if err := a.ensureProviderUsable(reqCtx(c), provider); err != nil {
		respondErr(c, err)
		return
	}
	if a.RechargeGateway == nil {
		respondErr(c, apperr.New("RECHARGE_UNAVAILABLE", "充值服务未装配", http.StatusServiceUnavailable))
		return
	}

	actualPaid := actualPaidCNY(body.AmountUSD, operation_setting.USDExchangeRate)
	order, err := a.RechargeGateway.CreateOrder(reqCtx(c), payment.OrderInput{
		Type:       payment.OrderTypeRecharge,
		TenantID:   t.ID,
		UserID:     userID,
		Provider:   provider,
		AmountUSD:  body.AmountUSD,
		ActualPaid: actualPaid,
		Subject:    fmt.Sprintf("钱包充值 $%g", body.AmountUSD),
	})
	if err != nil {
		respondErr(c, err)
		return
	}

	pay := gin.H{}
	switch provider {
	case payment.ProviderWxpay:
		pay["wxpay_qr"] = order.PayURL // 前端渲染二维码
	case payment.ProviderAlipay:
		pay["alipay_url"] = order.PayURL // 前端跳转
	}
	respondOK(c, gin.H{
		"order_no":   order.OrderNo,
		"amount_usd": body.AmountUSD,
		"amount_cny": actualPaid,
		"provider":   string(provider),
		"pay":        pay,
	})
}

// HandleWalletRechargeStatus GET /api/tenant/wallet/recharge/status?order_no=... —— 充值订单支付状态查询。
// 需 UserAuth。前端扫码支付（微信 native 无服务端跳转）后靠此端点轮询探活，探到已支付即结束轮询、
// 刷新余额（修「付完款不跳转」：原先只弹二维码、无状态轮询、无支付后动作）。
//
// **越权红线**：订单只可被其归属用户本人查询——跨用户一律回落 payment.ErrOrderNotFound（404），
// 不区分「订单不存在」与「订单存在但不是你的」，避免通过状态码差异枚举他人 order_no（对齐
// ticket 模块 HandleUserGetTicket 的 IDOR 处理范式）。
func (a *App) HandleWalletRechargeStatus(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Query("order_no"))
	if orderNo == "" {
		respondErr(c, errRechargeOrderNoRequired)
		return
	}
	if a.RechargeGateway == nil {
		respondErr(c, apperr.New("RECHARGE_UNAVAILABLE", "充值服务未装配", http.StatusServiceUnavailable))
		return
	}
	ord, err := a.RechargeGateway.GetByOrderNo(reqCtx(c), orderNo)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 越权红线：仅订单归属用户本人可查；不匹配一律当「不存在」处理，绝不泄露他人订单状态。
	if ord.UserID != int64(c.GetInt("id")) {
		respondErr(c, payment.ErrOrderNotFound)
		return
	}
	respondOK(c, gin.H{
		"paid":   ord.Status == payment.OrderPaid || ord.Status == payment.OrderCredited,
		"status": string(ord.Status),
	})
}
