package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// Gateway 同时实现 PaymentGateway（下单）与 CallbackHandler（回调入账分发）。
type Gateway struct {
	repo       OrderRepo
	sdk        PaySDK
	sinks      map[OrderType]OrderSink
	notifyBase string
	newOrderNo func() string
	now        func() time.Time
	logf       func(format string, args ...any)
}

var (
	_ PaymentGateway  = (*Gateway)(nil)
	_ CallbackHandler = (*Gateway)(nil)
)

type Option func(*Gateway)

func WithNotifyBaseURL(base string) Option  { return func(g *Gateway) { g.notifyBase = base } }
func WithClock(now func() time.Time) Option { return func(g *Gateway) { g.now = now } }
func WithOrderNoFunc(fn func() string) Option {
	return func(g *Gateway) { g.newOrderNo = fn }
}
func WithErrorLogf(fn func(format string, args ...any)) Option {
	return func(g *Gateway) {
		if fn != nil {
			g.logf = fn
		}
	}
}

func NewGateway(repo OrderRepo, sdk PaySDK, sinks map[OrderType]OrderSink, opts ...Option) *Gateway {
	g := &Gateway{
		repo:       repo,
		sdk:        sdk,
		sinks:      sinks,
		newOrderNo: defaultOrderNo,
		now:        time.Now,
		logf:       func(string, ...any) {},
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Gateway) notifyURL(p Provider) string {
	return g.notifyBase + p.NotifyPath()
}

// IdempotencyKeyMaxLen 幂等键应用层长度上限（三库 varchar 行为一致）。
const IdempotencyKeyMaxLen = 64

// validateIdempotencyKey 应用层格式校验（P0-5）。
func validateIdempotencyKey(key string) error {
	if key == "" {
		return nil
	}
	if len(key) > IdempotencyKeyMaxLen {
		return ErrOrderInvalid
	}
	// 仅允许可打印 ASCII（避免控制字符 / 超长 unicode 在三库表现不一）
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x21 || c > 0x7e {
			return ErrOrderInvalid
		}
	}
	return nil
}

// CreateOrder 下单（PAY-LAT-02 / PAY-IDEM-01 / Phase D）：
//
//	校验 → 幂等复用（含意图校验）→ 落 created → CreatePay
//	→ success 且 pay_url 落库成功才返回成功
//	→ definitive_reject → failed
//	→ outcome_unknown → 返回 (order, PAY_CREATE_UNKNOWN)，保持 created + 调度查单
func (g *Gateway) CreateOrder(ctx context.Context, in OrderInput) (*PayOrder, error) {
	t0 := g.now()
	g.logStage(StageRequestReceived, "", in.Provider, 0, "")

	if err := in.validate(); err != nil {
		return nil, err
	}
	idemKey := strings.TrimSpace(in.IdempotencyKey)
	if err := validateIdempotencyKey(idemKey); err != nil {
		return nil, err
	}
	if idemKey != "" {
		if existing, err := g.repo.GetByIdempotencyKey(ctx, idemKey); err == nil {
			if reused, ok, rerr := g.reuseOrRecoverOrder(ctx, existing, in); ok {
				g.logStage(StageResponseSent, reused.OrderNo, in.Provider, elapsedMs(t0), "idempotent_reuse=1")
				return reused, nil
			} else if rerr != nil {
				// unknown / pay_url missing：仍返回 order 供前端轮询
				return existing, rerr
			}
			return nil, ErrIdempotencyConflict
		} else if err != ErrOrderNotFound {
			return nil, err
		}
	}

	now := g.now()
	fen := YuanToFen(in.ActualPaid)
	orderNo := g.newOrderNo()
	exp := now.Add(DefaultQRValidity)
	o := &PayOrder{
		OrderNo:        orderNo,
		Type:           in.Type,
		TenantID:       in.TenantID,
		UserID:         in.UserID,
		Provider:       in.Provider,
		AmountUSD:      in.AmountUSD,
		ActualPaid:     in.ActualPaid,
		ActualPaidFen:  fen,
		GroupID:        in.GroupID,
		PlanID:         in.PlanID,
		Subject:        in.Subject,
		Reference:      in.Reference,
		IdempotencyKey: idemKey,
		Status:         OrderCreated,
		CreateState:    CreateStateLocalCreated,
		RootOrderNo:    orderNo,
		AttemptNo:      1,
		ActiveOrderNo:  orderNo,
		NotifyURL:      g.notifyURL(in.Provider),
		ExpiresAt:      exp,
		NextQueryAt:    NextQueryAtForAttempt(now, 0, now),
		QueryAttempts:  0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := g.repo.Create(ctx, o); err != nil {
		if err == ErrOrderDuplicate && idemKey != "" {
			if existing, gErr := g.repo.GetByIdempotencyKey(ctx, idemKey); gErr == nil {
				if reused, ok, rerr := g.reuseOrRecoverOrder(ctx, existing, in); ok {
					return reused, nil
				} else if rerr != nil {
					return existing, rerr
				}
				return nil, ErrIdempotencyConflict
			}
		}
		return nil, err
	}
	g.logStage(StageLocalOrderCreated, o.OrderNo, in.Provider, elapsedMs(t0), "")

	return g.finishCreatePay(ctx, o, in, t0)
}

// prepayLeaseSync 覆盖同步 overall(2.5s)+grace；prepayLeaseBg 覆盖后台 overall(20s)+grace。
// 期间 Query 不得 claim。
const (
	prepayLeaseSync = 5 * time.Second
	prepayLeaseBg   = 25 * time.Second
)

// prepayLeaseFor 按 context 预算模式选租约。
func prepayLeaseFor(ctx context.Context) time.Duration {
	// 避免 import realpay 环：用 context value 约定（realpay.WithBudgetMode 写入同 key 类型不可见）
	// 故用 Gateway 侧平行标记。
	if v := ctx.Value(ctxKeyPrepayBg{}); v != nil {
		return prepayLeaseBg
	}
	return prepayLeaseSync
}

type ctxKeyPrepayBg struct{}

// WithBackgroundPrepay 标记后台补下单（租约 25s）。mtwire 驱动器调用。
func WithBackgroundPrepay(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyPrepayBg{}, true)
}

func isSyncPrepay(ctx context.Context) bool {
	return ctx.Value(ctxKeyPrepayBg{}) == nil
}

// breakerAllowSync / breakerRecord* 由 realpay 注入，避免 payment→realpay 循环依赖。
// 默认允许（单测 / 未装配 realpay 时）。
var (
	breakerAllowSync = func() bool { return true }
	breakerOnSuccess = func() {}
	breakerOnUnknown = func() {}
	breakerOnReject  = func() {}
)

// SetBreakerHooks 由 mtwire 在装配时注入 realpay 熔断（可测、可关）。
func SetBreakerHooks(allow func() bool, onSuccess, onUnknown, onReject func()) {
	if allow != nil {
		breakerAllowSync = allow
	}
	if onSuccess != nil {
		breakerOnSuccess = onSuccess
	}
	if onUnknown != nil {
		breakerOnUnknown = onUnknown
	}
	if onReject != nil {
		breakerOnReject = onReject
	}
}

// finishCreatePay 唯一 Prepay 路径：ClaimForPrepay → 单次 SDK → FinishPrepayFenced(token)。
func (g *Gateway) finishCreatePay(ctx context.Context, o *PayOrder, in OrderInput, t0 time.Time) (*PayOrder, error) {
	// 已有可交付凭据
	if strings.TrimSpace(o.PayURL) != "" {
		return o, nil
	}
	// 有效 inflight lease：HTTP 重试返回 202，不调用 SDK
	now := g.now()
	if o.CreateState == CreateStatePrepayInflight && !o.RecoveryClaimUntil.IsZero() && o.RecoveryClaimUntil.After(now) {
		g.logStage(StageResponseSent, o.OrderNo, in.Provider, elapsedMs(t0), "outcome=inflight_lease")
		return o, ErrCreateOutcomeUnknown
	}

	// P1-B 熔断：开路时同步路径跳过 Prepay（<100ms 返回 queued）；后台驱动器仍可半开探测
	if isSyncPrepay(ctx) && !breakerAllowSync() {
		g.logStage(StageResponseSent, o.OrderNo, in.Provider, elapsedMs(t0), "outcome=breaker_open queued")
		return o, ErrCreateOutcomeUnknown
	}

	leaseUntil := now.Add(prepayLeaseFor(ctx))
	token, claimed, err := g.repo.ClaimForPrepay(ctx, o.OrderNo, now, leaseUntil)
	if err != nil {
		return o, err
	}
	if !claimed {
		cur, gErr := g.repo.GetByOrderNo(ctx, o.OrderNo)
		if gErr != nil {
			return o, gErr
		}
		if strings.TrimSpace(cur.PayURL) != "" {
			return cur, nil
		}
		// 他人持有 lease 或状态不允许：返回 order_no + 202 语义
		return cur, ErrCreateOutcomeUnknown
	}
	o.CreateState = CreateStatePrepayInflight

	tPay := g.now()
	cred, err := g.sdk.CreatePay(ctx, PayRequest{
		Provider:   in.Provider,
		OrderNo:    o.OrderNo,
		AmountUSD:  in.AmountUSD,
		ActualPaid: in.ActualPaid,
		Subject:    in.Subject,
		NotifyURL:  o.NotifyURL,
		ExpiresAt:  o.ExpiresAt,
	})
	oe := AsOutcome(err)
	attempt, maxA := 0, 0
	if oe != nil {
		attempt, maxA = oe.Attempt, oe.MaxAttempts
	}
	if err != nil {
		class, stage, outcomeStr := "unknown", "roundtrip", "unknown"
		if oe != nil {
			class, stage = oe.ErrorClass, oe.Stage
			outcomeStr = string(oe.Outcome)
		}
		g.logStage(StageProviderAttemptEnd, o.OrderNo, in.Provider, elapsedMs(tPay),
			"ok=0 attempt="+itoa(attempt)+" max_attempts="+itoa(maxA)+" outcome="+outcomeStr+" error_class="+class)

		if IsDefinitiveReject(err) {
			applied, fErr := g.repo.FinishPrepayFenced(ctx, o.OrderNo, token, CreateStateDefinitiveReject, "", time.Time{}, class, stage, max(attempt, 1))
			if fErr != nil {
				g.logf("payment: create %s: finish reject failed: %v", o.OrderNo, fErr)
			}
			if !applied {
				g.logf("payment: create %s: stale reject ignored", o.OrderNo)
			}
			breakerOnReject()
			// 对外必须是 AppError（PAY_PROVIDER_NO_AUTH 等），禁止 INTERNAL + 原始 SDK 串
			return o, MapCreateError(err)
		}
		// unknown：token 匹配才写 prepay_unknown + 调度
		next := g.now().Add(5 * time.Second)
		applied, fErr := g.repo.FinishPrepayFenced(ctx, o.OrderNo, token, CreateStatePrepayUnknown, "", next, class, stage, max(attempt, 1))
		if fErr != nil || !applied {
			g.logf("payment: create %s: finish unknown applied=%v err=%v", o.OrderNo, applied, fErr)
		}
		breakerOnUnknown()
		g.logStage(StageResponseSent, o.OrderNo, in.Provider, elapsedMs(t0), "outcome=unknown keep=created")
		return o, ErrCreateOutcomeUnknown
	}

	g.logStage(StageProviderAttemptEnd, o.OrderNo, in.Provider, elapsedMs(tPay),
		"ok=1 attempt="+itoa(attempt)+" max_attempts="+itoa(maxA)+" outcome=success")

	if cred == nil || strings.TrimSpace(cred.PayURL) == "" {
		next := g.now().Add(5 * time.Second)
		_, _ = g.repo.FinishPrepayFenced(ctx, o.OrderNo, token, CreateStatePrepayUnknown, "", next, "empty_code_url", "response", 1)
		return o, ErrCreateOutcomeUnknown
	}

	// success：token 匹配落库
	next := g.now().Add(5 * time.Second)
	applied, fErr := g.repo.FinishPrepayFenced(ctx, o.OrderNo, token, CreateStateCredentialReady, cred.PayURL, next, "", "", max(attempt, 1))
	if fErr != nil {
		g.logf("payment: create %s: persist pay_url failed: %v", o.OrderNo, fErr)
		return o, ErrPayURLPersist
	}
	if !applied {
		// stale：不得写回内存 QR 当成功
		g.logf("payment: create %s: stale success ignored", o.OrderNo)
		return o, ErrCreateOutcomeUnknown
	}
	o.PayURL = cred.PayURL
	o.CreateState = CreateStateCredentialReady
	breakerOnSuccess()
	g.logStage(StagePayURLPersisted, o.OrderNo, in.Provider, elapsedMs(t0), "")
	g.logStage(StageResponseSent, o.OrderNo, in.Provider, elapsedMs(t0), "outcome=success")
	return o, nil
}

// samePaymentIntent 校验幂等键复用时不可变支付意图一致（P0-5）。
func samePaymentIntent(existing *PayOrder, in OrderInput) bool {
	if existing == nil {
		return false
	}
	if existing.TenantID != in.TenantID || existing.UserID != in.UserID {
		return false
	}
	if existing.Type != in.Type || existing.Provider != in.Provider {
		return false
	}
	if existing.AmountUSD != in.AmountUSD {
		return false
	}
	if OrderActualPaidFen(existing) != YuanToFen(in.ActualPaid) {
		return false
	}
	return true
}

// reuseOrRecoverOrder 幂等复用：意图校验 → 有 pay_url 返回；无 pay_url 可尝试同单 Prepay 恢复。
func (g *Gateway) reuseOrRecoverOrder(ctx context.Context, existing *PayOrder, in OrderInput) (*PayOrder, bool, error) {
	if existing == nil {
		return nil, false, nil
	}
	if !samePaymentIntent(existing, in) {
		return nil, false, ErrIdempotencyConflict
	}
	switch existing.Status {
	case OrderCreated:
		if !existing.ExpiresAt.IsZero() && existing.ExpiresAt.Before(g.now()) {
			return nil, false, nil
		}
		if strings.TrimSpace(existing.PayURL) != "" {
			return existing, true, nil
		}
		// 无 QR：禁止空二维码成功。同 order_no 再试 Prepay（不得新开第二可支付单）。
		// 不得假设重复 Prepay 返回原 code_url。
		g.logf("payment: create recover %s: pending without pay_url; retry same out_trade_no", existing.OrderNo)
		o2, err := g.finishCreatePay(ctx, existing, in, g.now())
		if err == nil && o2 != nil && strings.TrimSpace(o2.PayURL) != "" {
			return o2, true, nil
		}
		if err == nil {
			return existing, false, ErrPayURLMissing
		}
		// 保留 NO_AUTH/业务拒绝等 AppError；禁止一律吞成 PAY_CREATE_UNKNOWN
		mapped := MapCreateError(err)
		code := apperr.CodeOf(mapped)
		if code == CodeProviderNoAuth || code == CodeProviderReject {
			return existing, false, mapped
		}
		if errors.Is(mapped, ErrCreateOutcomeUnknown) || code == CodeCreateOutcomeUnknown {
			return existing, false, ErrCreateOutcomeUnknown
		}
		if errors.Is(mapped, ErrPayURLPersist) || errors.Is(mapped, ErrPayURLMissing) {
			return existing, false, mapped
		}
		_ = g.repo.ScheduleNextQuery(ctx, existing.OrderNo, g.now().Add(5*time.Second), existing.QueryAttempts)
		return existing, false, ErrCreateOutcomeUnknown
	case OrderPaid, OrderCredited:
		return nil, false, ErrIdempotencyConflict
	default:
		return nil, false, ErrIdempotencyConflict
	}
}

func (g *Gateway) GetByOrderNo(ctx context.Context, orderNo string) (*PayOrder, error) {
	return g.repo.GetByOrderNo(ctx, orderNo)
}

func defaultOrderNo() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "PAY" + strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:])
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
