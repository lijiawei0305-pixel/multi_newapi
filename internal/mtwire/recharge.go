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
	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
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
	// errRechargeUserMissing 充值入账时目标用户行**根本不存在**（连 Unscoped 绕软删也 0 行匹配）。
	// 返回它使整个入账事务回滚（台账 / quota / TopUp 一并撤销）、订单不推进 credited，回调 ack 非 SUCCESS
	// → 平台重推 + 对账兜底重试 + 失败告警，绝不静默把这笔钱吞进「已入账」的假象。
	errRechargeUserMissing = apperr.New("RECHARGE_USER_MISSING", "充值入账目标用户不存在", http.StatusInternalServerError)
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

// rechargeCreditLedgerRow 是充值入账幂等台账（每 attempt order_no 至多一条）。
// Phase F：权威比较用 integer quota + actual_paid_fen，禁止 float 精确相等。
// amount_usd 与 payment_orders 对齐 decimal(20,8)，仅审计展示。
type rechargeCreditLedgerRow struct {
	OrderNo       string    `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	RootOrderNo   string    `gorm:"column:root_order_no;type:varchar(64);not null;default:'';index:idx_mt_rcl_root"`
	TenantID      int64     `gorm:"column:tenant_id;not null;index"`
	UserID        int64     `gorm:"column:user_id;not null;index"`
	Quota         int64     `gorm:"column:quota;not null"` // 权威入账额度（整数）
	ActualPaidFen int64     `gorm:"column:actual_paid_fen;not null;default:0"`
	AmountUSD     float64   `gorm:"column:amount_usd;type:decimal(20,8);not null;default:0"` // 审计；与订单同精度
	CreatedAt     time.Time `gorm:"column:created_at"`
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
type rechargeQuotaSink struct {
	db *gorm.DB
	// alerter 可选（nil 安全）：命中软删用户入账时经此发 Critical 告警知会风控（退款 / 恢复账号核查）。
	alerter alert.AlertSink
}

// errLedgerDuplicate 是「ledger 主键冲突」专用 sentinel。
// 在事务内返回它使整事务回滚（含 PostgreSQL aborted 语义），
// 事务外再读既有 ledger 校验后按幂等成功处理——**绝不**用 RowsAffected 判所有权
// （MySQL clientFoundRows=true 下 ON DUPLICATE KEY 可能 RowsAffected=1，见 Phase E P0-DB-01）。
var errLedgerDuplicate = errors.New("mtwire: recharge ledger already exists")

// errRechargeLedgerInvariant ledger 字段与本次 PaidOrder 不一致（资金不变量）。
var errRechargeLedgerInvariant = apperr.New("RECHARGE_LEDGER_INVARIANT", "充值台账与订单意图不一致", http.StatusConflict)

// errRechargeTopUpInvariant TopUp 与本次意图不一致。
var errRechargeTopUpInvariant = apperr.New("RECHARGE_TOPUP_INVARIANT", "充值账单与订单意图不一致", http.StatusConflict)

// errRechargeQuotaInvalid q<=0 不得入账后标 credited。
var errRechargeQuotaInvalid = apperr.New("RECHARGE_QUOTA_INVALID", "充值额度无效", http.StatusBadRequest)

// errRechargeTopUpMissing ledger 存在但 TopUp 缺失且无法安全补建。
var errRechargeTopUpMissing = apperr.New("RECHARGE_TOPUP_MISSING", "充值账单缺失，待恢复", http.StatusConflict)

func (s rechargeQuotaSink) OnPaid(ctx context.Context, o payment.PaidOrder) error {
	q := rechargeQuota(o.AmountUSD)
	if q <= 0 {
		return errRechargeQuotaInvalid
	}
	wantFen := o.ActualPaidFen
	if wantFen <= 0 {
		wantFen = payment.YuanToFen(o.ActualPaid)
	}
	root := o.RootOrderNo
	if root == "" {
		root = o.OrderNo
	}
	// TODO(recharge_spread)：差价分润待数据模型。
	softDeletedCredited := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// P0-DB-01：普通 INSERT；唯一冲突 → sentinel 整事务回滚；禁止 RowsAffected 所有权。
		ledger := rechargeCreditLedgerRow{
			OrderNo:       o.OrderNo,
			RootOrderNo:   root,
			TenantID:      o.TenantID,
			UserID:        o.UserID,
			Quota:         int64(q),
			ActualPaidFen: wantFen,
			AmountUSD:     o.AmountUSD,
			CreatedAt:     time.Now(),
		}
		if err := tx.Create(&ledger).Error; err != nil {
			if isDuplicateLedgerErr(err) {
				return errLedgerDuplicate
			}
			return err
		}
		uresQ := tx.Model(&model.User{}).Where("id = ?", o.UserID).
			Update("quota", gorm.Expr("quota + ?", q))
		if uresQ.Error != nil {
			return uresQ.Error
		}
		if uresQ.RowsAffected == 0 {
			ures := tx.Unscoped().Model(&model.User{}).Where("id = ?", o.UserID).
				Update("quota", gorm.Expr("quota + ?", q))
			if ures.Error != nil {
				return ures.Error
			}
			if ures.RowsAffected == 0 {
				return errRechargeUserMissing
			}
			softDeletedCredited = true
		}
		if err := tx.Create(&model.TopUp{
			UserId:          int(o.UserID),
			Amount:          int64(math.Round(o.AmountUSD)),
			Money:           o.ActualPaid,
			TradeNo:         o.OrderNo,
			PaymentMethod:   rechargePaymentMethod(o.Provider),
			PaymentProvider: string(o.Provider),
			CreateTime:      time.Now().Unix(),
			CompleteTime:    time.Now().Unix(),
			Status:          common.TopUpStatusSuccess,
		}).Error; err != nil {
			if isDuplicateLedgerErr(err) {
				return fmt.Errorf("topup unique after ledger claim: %w", err)
			}
			return err
		}
		if err := enqueueCacheInvalidationOutboxTx(tx, o.OrderNo, o.UserID); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, errLedgerDuplicate) {
		return s.finishIdempotentLedgerHit(ctx, o, int64(q), wantFen, root)
	}
	if err != nil {
		return err
	}
	// Redis 删除在提交后立即执行；失败由 outbox 循环补失效。
	if cErr := model.InvalidateUserCache(int(o.UserID)); cErr != nil {
		common.SysLog("recharge credit: invalidate user cache failed (order " + o.OrderNo + "): " + cErr.Error())
	} else {
		markCacheInvalidationOutboxDone(s.db, o.OrderNo)
	}
	if softDeletedCredited {
		// 异常路径：用户在回调落地前被软删，额度已 Unscoped 落到其行（可恢复、不丢账）。留持久日志痕迹
		// 并经 AlertSink 发 Critical 告警知会风控核查（退款 / 恢复账号）。best-effort：绝不影响入账结果，
		// 幂等台账已保证同单只到此一次，故告警亦只发一次（DedupKey 再兜底防并发/重推重复分发）。
		msg := fmt.Sprintf("充值入账命中软删用户：order=%s user=%d quota=%d ¥%.2f（额度已落其行、可恢复；请风控核查是否退款/恢复账号）",
			o.OrderNo, o.UserID, q, o.ActualPaid)
		common.SysLog(msg)
		if s.alerter != nil {
			_ = s.alerter.Dispatch(ctx, alert.Alert{
				Level:    alert.LevelCritical,
				Subject:  "充值入账命中软删用户",
				Body:     msg,
				DedupKey: "recharge_softdeleted_credit:" + o.OrderNo,
			})
		}
	}
	return nil
}

// rechargePaymentMethod 把支付渠道映射为账单历史展示用的 payment_method 取值。
// "_official" 后缀是既有约定（见 operation_setting.OfficialPayMethodTypes /
// 前端 features/wallet/lib/billing.ts 的 PAYMENT_METHOD_NAMES），用来把「主站进程内官方
// 微信/支付宝 SDK」与 Epay 网关的裸 "wxpay"/"alipay" 区分开；前端据此渲染
// "Official WeChat Pay"/"Official Alipay"（zh 本地化："官方微信支付"/"官方支付宝"）。
func rechargePaymentMethod(p payment.Provider) string {
	return string(p) + "_official"
}

// isDuplicateLedgerErr 报告是否为唯一约束/主键冲突错误（跨 MySQL/sqlite/PostgreSQL，driver 无关）。
func isDuplicateLedgerErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, frag := range []string{
		"duplicate entry", "unique constraint", "duplicate key", "duplicated key",
		"1062", "unique violation", "sqlstate 23505",
	} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

// finishIdempotentLedgerHit：ledger 冲突后事务外校验（整数 quota/fen，禁止 float==）。
// TopUp 缺失：不增加 quota 的前提下安全补建；查询错误不得吞掉。
func (s rechargeQuotaSink) finishIdempotentLedgerHit(ctx context.Context, o payment.PaidOrder, wantQuota, wantFen int64, root string) error {
	var row rechargeCreditLedgerRow
	if err := s.db.WithContext(ctx).Take(&row, "order_no = ?", o.OrderNo).Error; err != nil {
		return err
	}
	// 权威比较：quota + actual_paid_fen + tenant/user/root（禁止 float AmountUSD）
	if row.TenantID != o.TenantID || row.UserID != o.UserID || row.Quota != wantQuota {
		return s.ledgerInvariantAlert(ctx, o, row, wantQuota, wantFen)
	}
	if row.ActualPaidFen > 0 && wantFen > 0 && row.ActualPaidFen != wantFen {
		return s.ledgerInvariantAlert(ctx, o, row, wantQuota, wantFen)
	}
	if row.RootOrderNo != "" && root != "" && row.RootOrderNo != root {
		return s.ledgerInvariantAlert(ctx, o, row, wantQuota, wantFen)
	}

	var top model.TopUp
	err := s.db.WithContext(ctx).Where("trade_no = ?", o.OrderNo).Take(&top).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err // 查询错误不得吞掉
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 不增加 quota，安全补建 TopUp（ledger 已证明入账过）
		if cErr := s.db.WithContext(ctx).Create(&model.TopUp{
			UserId:          int(o.UserID),
			Amount:          int64(math.Round(o.AmountUSD)),
			Money:           o.ActualPaid,
			TradeNo:         o.OrderNo,
			PaymentMethod:   rechargePaymentMethod(o.Provider),
			PaymentProvider: string(o.Provider),
			CreateTime:      time.Now().Unix(),
			CompleteTime:    time.Now().Unix(),
			Status:          common.TopUpStatusSuccess,
		}).Error; cErr != nil {
			if !isDuplicateLedgerErr(cErr) {
				common.SysLog("recharge topup repair failed order=" + o.OrderNo + ": " + cErr.Error())
				return errRechargeTopUpMissing
			}
		}
	} else {
		if top.UserId != int(o.UserID) || top.Status != common.TopUpStatusSuccess ||
			top.Amount != int64(math.Round(o.AmountUSD)) ||
			(o.Provider != "" && top.PaymentProvider != "" && top.PaymentProvider != string(o.Provider)) {
			msg := fmt.Sprintf("recharge topup invariant: order=%s topup user=%d amount=%d status=%s",
				o.OrderNo, top.UserId, top.Amount, top.Status)
			common.SysLog(msg)
			if s.alerter != nil {
				_ = s.alerter.Dispatch(ctx, alert.Alert{
					Level: alert.LevelCritical, Subject: "充值账单与订单意图不一致",
					Body: msg, DedupKey: "recharge_topup_invariant:" + o.OrderNo,
				})
			}
			return errRechargeTopUpInvariant
		}
	}

	// outbox：缺失可靠补建；冲突校验 order_no/user_id
	if err := enqueueCacheInvalidationOutboxTx(s.db, o.OrderNo, o.UserID); err != nil {
		if !isDuplicateLedgerErr(err) {
			return err
		}
	}
	if cErr := model.InvalidateUserCache(int(o.UserID)); cErr != nil {
		common.SysLog("recharge credit: invalidate user cache failed (order " + o.OrderNo + "): " + cErr.Error())
	} else {
		markCacheInvalidationOutboxDone(s.db, o.OrderNo)
	}
	return nil
}

func (s rechargeQuotaSink) ledgerInvariantAlert(ctx context.Context, o payment.PaidOrder, row rechargeCreditLedgerRow, wantQuota, wantFen int64) error {
	msg := fmt.Sprintf("recharge ledger invariant: order=%s have(tenant=%d user=%d q=%d fen=%d root=%s) want(tenant=%d user=%d q=%d fen=%d)",
		o.OrderNo, row.TenantID, row.UserID, row.Quota, row.ActualPaidFen, row.RootOrderNo,
		o.TenantID, o.UserID, wantQuota, wantFen)
	common.SysLog(msg)
	if s.alerter != nil {
		_ = s.alerter.Dispatch(ctx, alert.Alert{
			Level: alert.LevelCritical, Subject: "充值台账与订单意图不一致",
			Body: msg, DedupKey: "recharge_ledger_invariant:" + o.OrderNo,
		})
	}
	return errRechargeLedgerInvariant
}

// rechargeQuota 把充值美元额折算为 new-api 内部 quota 单位（$1 = common.QuotaPerUnit；截断）。
// int 返回值供充值入账（IncreaseUserQuota）使用；换算走统一核心 usdToQuotaRound（见 money.go）。
func rechargeQuota(amountUSD float64) int {
	return int(usdToQuotaRound(amountUSD, roundDown))
}

// actualPaidCNY 计算用户实付人民币 = 美元额 × 汇率（收益币种；差价基准）。
func actualPaidCNY(amountUSD, rate float64) float64 {
	return amountUSD * rate
}

// resolveRechargeAmount 归一充值金额口径（纯函数，供 handler 调用 + 单测锁定）。
// 返回 (美元额, 实付¥, 是否人民币口径)：
//   - amountCNY>0：人民币充值（所见即所付）——实付=该¥精确到分、usd=cny/rate；
//   - 否则：美元充值（旧口径）——usd=amountUSD、实付=usd×rate。
//
// rate<=0 兜底为 1（防除零/负汇率）。入账始终以返回的 usd 折原生 quota（$1=QuotaPerUnit），
// 无论走哪路，计费/入账数学都不变——本函数只决定「实付¥」与「入账美元额」的换算口径。
func resolveRechargeAmount(amountUSD, amountCNY, rate float64) (usd, actualPaid float64, cnyMode bool) {
	if rate <= 0 {
		rate = 1
	}
	if amountCNY > 0 {
		return amountCNY / rate, amountCNY, true
	}
	return amountUSD, actualPaidCNY(amountUSD, rate), false
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
//
// 两种金额口径（二选一）：
//   - AmountCNY>0：按**人民币**充值（所见即所付）。实付即此值、精确到分；amount_usd = cny/汇率。
//     微信/支付宝本就以 ¥ 结算，中国用户按元充值最自然，优先走这一路。
//   - 否则回退 AmountUSD：按美元充值（旧口径），实付 = usd×汇率。
//
// 两路最终都归一到 (amountUSD, actualPaid¥)，入账仍以 amountUSD 折原生 quota（$1=QuotaPerUnit），计费数学不变。
type rechargeRequest struct {
	AmountUSD      float64 `json:"amount_usd"`
	AmountCNY      float64 `json:"amount_cny"`      // 可选：人民币充值，>0 时优先，实付精确到分
	Provider       string  `json:"provider"`        // wxpay | alipay
	IdempotencyKey string  `json:"idempotency_key"` // 可选：客户端支付意图幂等键（网络重试复用）
}

// HandleWalletRecharge POST /api/tenant/wallet/recharge —— 钱包充值下单。需 UserAuth + Host 租户
// （主站 Host 无租户但命中 tenant.IsMainSiteHost 时回退平台租户，见 http.go resolveBuyerTenant——
// 产品侧已确认「主站自身也接受终端用户直充」，与买家套餐购买同一 main-site direct-sales 口径）。
//
// 流程：金额口径归一（amount_cny>0 走人民币充值、实付=该¥精确到分、usd=cny/汇率；否则美元充值、实付=usd×汇率）
// → 校验 usd≥$1 与渠道 → RechargeGateway.CreateOrder（落库 RCG 订单 + 进程内向平台下单拿支付凭据）
// → 返回 {order_no, amount_*, provider, pay:{wxpay_qr|alipay_url}}。
func (a *App) HandleWalletRecharge(c *gin.Context) {
	t, err := a.resolveBuyerTenant(c)
	if err != nil {
		respondErr(c, err)
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
	// 金额口径归一（可测纯函数）：amount_cny>0 走人民币充值（实付精确到分，usd=cny/汇率）；否则走美元充值（实付=usd×汇率）。
	amountUSD, actualPaid, cnyMode := resolveRechargeAmount(body.AmountUSD, body.AmountCNY, operation_setting.USDExchangeRate)
	if amountUSD < minRechargeUSD {
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

	subject := fmt.Sprintf("钱包充值 $%g", amountUSD)
	if cnyMode {
		subject = fmt.Sprintf("钱包充值 ¥%.2f", actualPaid)
	}
	order, err := a.RechargeGateway.CreateOrder(reqCtx(c), payment.OrderInput{
		Type:           payment.OrderTypeRecharge,
		TenantID:       t.ID,
		UserID:         userID,
		Provider:       provider,
		AmountUSD:      amountUSD,
		ActualPaid:     actualPaid,
		Subject:        subject,
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
	})
	// P0-4：outcome_unknown / pay_url 落库失败时 order 仍非 nil，须返回 order_no 供轮询
	if err != nil {
		if order != nil && (errors.Is(err, payment.ErrCreateOutcomeUnknown) || errors.Is(err, payment.ErrPayURLPersist) || errors.Is(err, payment.ErrPayURLMissing)) {
			respondCreateUnknown(c, order, amountUSD, actualPaid, provider, err)
			return
		}
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
	expiresAt := ""
	if !order.ExpiresAt.IsZero() {
		expiresAt = order.ExpiresAt.Format(time.RFC3339)
	}
	respondOK(c, gin.H{
		"order_no":        order.OrderNo,
		"amount_usd":      amountUSD,
		"amount_cny":      actualPaid,
		"amount_cny_fen":  payment.OrderActualPaidFen(order),
		"provider":        string(provider),
		"idempotency_key": order.IdempotencyKey,
		"expires_at":      expiresAt,
		"status":          string(order.Status),
		"pay":             pay,
	})
}

// respondCreateUnknown 返回 202 + order_no，前端可轮询 status 直至出现二维码或终态。
func respondCreateUnknown(c *gin.Context, order *payment.PayOrder, amountUSD, actualPaid float64, provider payment.Provider, err error) {
	expiresAt := ""
	if order != nil && !order.ExpiresAt.IsZero() {
		expiresAt = order.ExpiresAt.Format(time.RFC3339)
	}
	orderNo, status, idem := "", "created", ""
	if order != nil {
		orderNo = order.OrderNo
		status = string(order.Status)
		idem = order.IdempotencyKey
	}
	code := payment.CodeCreateOutcomeUnknown
	msg := "支付订单确认中，请稍后查询状态"
	if ae, ok := err.(*apperr.AppError); ok && ae != nil {
		code = ae.Code
		msg = ae.Msg
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success": false,
		"message": msg,
		"code":    code,
		"data": gin.H{
			"order_no":        orderNo,
			"status":          status,
			"amount_usd":      amountUSD,
			"amount_cny":      actualPaid,
			"provider":        string(provider),
			"idempotency_key": idem,
			"expires_at":      expiresAt,
			"poll_path":       "/api/tenant/wallet/recharge/status?order_no=" + orderNo,
			"pay":             gin.H{},
		},
	})
}

// rechargeQRValidity 微信 Native 二维码默认有效窗口（对账过期同口径 2h）。状态接口用它推算
// expires_at，供前端倒计时；无独立 DB 字段时以 CreatedAt+窗口为权威近似。
const rechargeQRValidity = 2 * time.Hour

// HandleWalletRechargeStatus GET /api/tenant/wallet/recharge/status?order_no=... —— 充值订单支付状态查询。
// 需 UserAuth。前端扫码支付（微信 native 无服务端跳转）后靠此端点轮询探活。
//
// 状态语义（PAY-STA-01）：
//   - provider_paid：支付机构已确认（status=paid 或 credited）
//   - credited：本站余额入账完成（仅 status=credited）
//   - paid（兼容旧客户端）：**仅当 credited 时为 true**，避免把入账中间态误判为到账完成
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
	// 越权红线：UserID + Host 解析 TenantID 任一不匹配 → 与不存在相同的 404
	if ord.UserID != int64(c.GetInt("id")) {
		respondErr(c, payment.ErrOrderNotFound)
		return
	}
	if t, tErr := a.resolveBuyerTenant(c); tErr == nil && t != nil && ord.TenantID != t.ID {
		respondErr(c, payment.ErrOrderNotFound)
		return
	}

	providerPaid := ord.Status == payment.OrderPaid || ord.Status == payment.OrderCredited
	credited := ord.Status == payment.OrderCredited
	creditedQuota := 0
	if credited {
		creditedQuota = rechargeQuota(ord.AmountUSD)
	}
	// current_quota 读主库权威值（fromDB=true），避免 Redis 缓存滞后导致前端拿到旧余额。
	// model.DB 未装配（纯内存网关单测）时跳过，返回 0。
	currentQuota := 0
	if model.DB != nil {
		if q, qErr := model.GetUserQuota(int(ord.UserID), true); qErr == nil {
			currentQuota = q
		}
	}

	expiresAt := ""
	if !ord.ExpiresAt.IsZero() {
		expiresAt = ord.ExpiresAt.Format(time.RFC3339)
	} else if !ord.CreatedAt.IsZero() {
		expiresAt = ord.CreatedAt.Add(rechargeQRValidity).Format(time.RFC3339)
	}
	updatedAt := ""
	if !ord.UpdatedAt.IsZero() {
		updatedAt = ord.UpdatedAt.Format(time.RFC3339)
	}
	providerPaidAt := ""
	if !ord.ProviderPaidAt.IsZero() {
		providerPaidAt = ord.ProviderPaidAt.Format(time.RFC3339)
	}
	creditedAt := ""
	if !ord.CreditedAt.IsZero() {
		creditedAt = ord.CreditedAt.Format(time.RFC3339)
	}

	// 仅 active + credential_ready + 未过期 返回 PayURL（不返回 failed/closed/replaced 旧码）
	pay := gin.H{}
	rootNo := ord.RootOrderNo
	if rootNo == "" {
		rootNo = ord.OrderNo
	}
	activeNo := ord.ActiveOrderNo
	if activeNo == "" {
		activeNo = ord.OrderNo
	}
	// 若查的是旧 attempt，跟随 active（同 root）
	display := ord
	if activeNo != "" && activeNo != ord.OrderNo && a.RechargeGateway != nil {
		if act, aErr := a.RechargeGateway.GetByOrderNo(reqCtx(c), activeNo); aErr == nil && act.UserID == ord.UserID {
			display = act
		}
	}
	// 任意 attempt 已 paid/credited → 资金事实优先
	if display.Status == payment.OrderPaid || display.Status == payment.OrderCredited {
		providerPaid = true
		credited = display.Status == payment.OrderCredited
	}
	canExposePay := display.Status == payment.OrderCreated &&
		(display.CreateState == payment.CreateStateCredentialReady || display.CreateState == "" && strings.TrimSpace(display.PayURL) != "") &&
		(display.ExpiresAt.IsZero() || display.ExpiresAt.After(time.Now())) &&
		strings.TrimSpace(display.PayURL) != ""
	if canExposePay {
		switch display.Provider {
		case payment.ProviderWxpay:
			pay["wxpay_qr"] = display.PayURL
		case payment.ProviderAlipay:
			pay["alipay_url"] = display.PayURL
		}
	}

	respondOK(c, gin.H{
		"order_no":             display.OrderNo,
		"root_order_no":        rootNo,
		"active_order_no":      activeNo,
		"status":               string(display.Status),
		"create_state":         string(display.CreateState),
		"provider_trade_state": display.ProviderTradeState,
		"provider_paid":        providerPaid || display.Status == payment.OrderPaid || display.Status == payment.OrderCredited,
		"credited":             credited || display.Status == payment.OrderCredited,
		"paid":                 credited || display.Status == payment.OrderCredited,
		"amount_cny":           display.ActualPaid,
		"amount_cny_fen":       payment.OrderActualPaidFen(display),
		"amount_usd":           display.AmountUSD,
		"credited_quota":       creditedQuota,
		"current_quota":        currentQuota,
		"expires_at":           expiresAt,
		"provider_paid_at":     providerPaidAt,
		"credited_at":          creditedAt,
		"updated_at":           updatedAt,
		"pay":                  pay,
	})
}
