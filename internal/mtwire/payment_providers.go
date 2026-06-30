package mtwire

// 支付渠道启用开关（Phase 2 · 微信/支付宝渠道管理）。
//
// 两个维度共同决定「买家能否用某渠道支付」：
//   - configured：来自 auth-service GET /auth/healthz 的 providers（真实凭据是否齐全；mock 模式两者皆 true）；
//   - enabled   ：管理员开关，存主站（mt_payment_provider_settings）。缺省（无记录）= true
//                 （不回归现有「两渠道都显示」行为）。
//
// 买家可用渠道 = enabled && configured。下单（HandleWalletRecharge）必须校验该渠道 enabled && configured。
//
// 本文件把「启用开关存储 + 渠道状态查询 seam + 三个 HTTP handler」集中于此，保持 recharge.go/http.go 干净。

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// 支付渠道相关错误码（沿用模块前缀约定）。
var (
	// errProviderInvalid provider 参数非 wxpay/alipay。
	errProviderInvalid = apperr.New("PROVIDER_INVALID", "未知支付渠道", http.StatusBadRequest)
	// errProviderDisabled 渠道未启用（管理员关闭）或未配置（真实凭据缺失），不可下单。
	errProviderDisabled = apperr.New("PROVIDER_DISABLED", "该支付渠道未启用或未配置", http.StatusBadRequest)
	// errProviderBadRequest 设开关请求体非法（缺 enabled 字段等）。
	errProviderBadRequest = apperr.New("PROVIDER_BAD_REQUEST", "请求参数非法", http.StatusBadRequest)
)

// ---- auth-service /auth/healthz 渠道配置查询（configured） ----

// healthzResponse 是 auth-service GET /auth/healthz 的响应契约（providers 在顶层）。
type healthzResponse struct {
	Success   bool `json:"success"`
	Mock      bool `json:"mock"`
	Providers struct {
		Wxpay  bool `json:"wxpay"`
		Alipay bool `json:"alipay"`
	} `json:"providers"`
}

// GetProviders 查 auth-service /auth/healthz，返回各渠道真实凭据是否齐全（configured）。
// mock 模式 wxpay/alipay 均 true。复用 c.http（8s 超时）。失败返回 err（调用方降级当作 false + 记日志，不 panic）。
func (c *authServiceClient) GetProviders(ctx context.Context) (wxpay bool, alipay bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/auth/healthz", nil)
	if err != nil {
		return false, false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, false, apperr.New("AUTH_SERVICE_UNREACHABLE", "支付服务暂不可用", http.StatusBadGateway).Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false, apperr.New("AUTH_SERVICE_QUERY_FAILED", "查询支付渠道失败", http.StatusBadGateway)
	}
	var out healthzResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, false, apperr.New("AUTH_SERVICE_BAD_RESPONSE", "支付服务响应异常", http.StatusBadGateway).Wrap(err)
	}
	return out.Providers.Wxpay, out.Providers.Alipay, nil
}

// providerStatusQuery 查各渠道真实凭据是否齐全（configured）；单测替换为桩（仿 subOrderPaidQuery seam）。
// authClient 未装配（如单测直构 App）时返回 errAuthClientUnset。
var providerStatusQuery = func(a *App, ctx context.Context) (wxpay bool, alipay bool, err error) {
	if a.authClient == nil {
		return false, false, errAuthClientUnset
	}
	return a.authClient.GetProviders(ctx)
}

// ---- 启用开关持久化（mt_payment_provider_settings） ----

// paymentProviderSetting 是支付渠道启用开关的持久化行（mt_ 前缀，避让原生表）。
// 缺省（无记录）语义 = enabled=true，由读取处（providerEnabled）处理。
type paymentProviderSetting struct {
	Provider string `gorm:"column:provider;primaryKey;type:varchar(16)"`
	// 不加 default:true —— 缺省 true 语义在读取处(providerEnabled)处理；DB 列加 default 会让 GORM
	// 把零值 false 当作"用默认"而写入 true，导致 setEnabled(false) 无法落库（曾踩此坑）。
	Enabled   bool      `gorm:"column:enabled;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名（mt_ 前缀）。
func (paymentProviderSetting) TableName() string { return "mt_payment_provider_settings" }

// migratePaymentProviders 建支付渠道启用开关表（由 App.Migrate 调用，仿 migrateSubscriptionBridge）。
func migratePaymentProviders(db *gorm.DB) error {
	return db.AutoMigrate(&paymentProviderSetting{})
}

// paymentProviderStore 读写渠道启用开关。
type paymentProviderStore struct{ db *gorm.DB }

func newPaymentProviderStore(db *gorm.DB) *paymentProviderStore {
	return &paymentProviderStore{db: db}
}

// getEnabled 读某渠道启用开关；exists=false 表示无记录（调用方按缺省 true 处理）。
func (s *paymentProviderStore) getEnabled(ctx context.Context, provider string) (enabled bool, exists bool) {
	var row paymentProviderSetting
	if err := s.db.WithContext(ctx).Take(&row, "provider = ?", provider).Error; err != nil {
		return false, false
	}
	return row.Enabled, true
}

// setEnabled upsert 某渠道启用开关（provider 主键冲突即更新 enabled+updated_at）。
func (s *paymentProviderStore) setEnabled(ctx context.Context, provider string, enabled bool) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(&paymentProviderSetting{
		Provider:  provider,
		Enabled:   enabled,
		UpdatedAt: time.Now(),
	}).Error
}

// providerEnabled 读渠道启用开关，无记录视为启用（缺省 true，保持既有「两渠道都显示」行为）。
func (s *paymentProviderStore) providerEnabled(ctx context.Context, provider string) bool {
	enabled, exists := s.getEnabled(ctx, provider)
	if !exists {
		return true
	}
	return enabled
}

// ---- 下单校验（HandleWalletRecharge 复用） ----

// ensureProviderUsable 校验某渠道可下单：enabled（管理员开关，缺省 true）且 configured（真实凭据齐全）。
//   - enabled 明确为 false → 一律拒绝（errProviderDisabled），且不再查 configured；
//   - configured 经 auth-service /auth/healthz 查询：查询出错时为不阻塞正常充值而放行（configured 未知→放行，
//     避免 auth-service 抖动误伤），仅当明确未配置才拒绝。
func (a *App) ensureProviderUsable(ctx context.Context, provider payment.Provider) error {
	if !newPaymentProviderStore(a.DB).providerEnabled(ctx, string(provider)) {
		return errProviderDisabled // enabled 明确为 false
	}
	wx, ali, err := providerStatusQuery(a, ctx)
	if err != nil {
		common.SysLog("recharge: query provider configured failed, allow through: " + err.Error())
		return nil // configured 未知 → 放行（不阻塞正常充值）
	}
	configured := wx
	if provider == payment.ProviderAlipay {
		configured = ali
	}
	if !configured {
		return errProviderDisabled // 明确未配置 → 拒绝
	}
	return nil
}

// ---- HTTP 处理器 ----

// providerStatusOut 是单渠道状态（configured=真实凭据齐全；enabled=管理员开关）。
type providerStatusOut struct {
	Configured bool `json:"configured"`
	Enabled    bool `json:"enabled"`
}

// HandleAdminListPaymentProviders GET /api/admin/payment/providers —— 列两渠道 configured/enabled。需 AdminAuth。
// auth-service 查询出错时 configured 当作 false 并记日志（不 500，仅影响「是否可下单」展示）。
func (a *App) HandleAdminListPaymentProviders(c *gin.Context) {
	ctx := reqCtx(c)
	wx, ali, err := providerStatusQuery(a, ctx)
	if err != nil {
		common.SysLog("payment providers: query auth-service failed: " + err.Error())
		wx, ali = false, false
	}
	store := newPaymentProviderStore(a.DB)
	respondOK(c, gin.H{
		"wxpay": providerStatusOut{
			Configured: wx,
			Enabled:    store.providerEnabled(ctx, string(payment.ProviderWxpay)),
		},
		"alipay": providerStatusOut{
			Configured: ali,
			Enabled:    store.providerEnabled(ctx, string(payment.ProviderAlipay)),
		},
	})
}

// setProviderRequest 是 PUT /api/admin/payment/providers/:provider 入参。
// Enabled 用指针以区分「显式 false」与「字段缺省」——缺省视为非法请求，避免空 body 误关渠道。
type setProviderRequest struct {
	Enabled *bool `json:"enabled"`
}

// HandleAdminSetPaymentProvider PUT /api/admin/payment/providers/:provider —— 设渠道启用开关。需 AdminAuth。
func (a *App) HandleAdminSetPaymentProvider(c *gin.Context) {
	provider := c.Param("provider")
	if !payment.Provider(provider).Valid() {
		respondErr(c, errProviderInvalid)
		return
	}
	var body setProviderRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.Enabled == nil {
		respondErr(c, errProviderBadRequest)
		return
	}
	if err := newPaymentProviderStore(a.DB).setEnabled(reqCtx(c), provider, *body.Enabled); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"provider": provider, "enabled": *body.Enabled})
}

// HandleTenantRechargeMethods GET /api/tenant/wallet/recharge/methods —— 当前租户买家可用充值渠道。
// 需 UserAuth + Host 租户。可用 = enabled（管理员开关，缺省 true）&& configured（真实凭据齐全）。
// auth-service 查询出错时**返回错误**（而非空列表）：前端据此 fail-open 回退展示两渠道，与下单
// 校验 ensureProviderUsable「configured 未知→放行」保持一致（避免 auth-service 抖动时 UI 隐藏卡而下单却放行的不一致）。
// 真正「两渠道都未配置」(无错、均 false) 才返回空列表 → 前端据此隐藏整卡（确实无法支付）。
func (a *App) HandleTenantRechargeMethods(c *gin.Context) {
	if tenantFrom(c) == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	ctx := reqCtx(c)
	wx, ali, err := providerStatusQuery(a, ctx)
	if err != nil {
		common.SysLog("recharge methods: query auth-service failed: " + err.Error())
		respondErr(c, err) // 出错→报错；前端 fail-open 回退两渠道
		return
	}
	store := newPaymentProviderStore(a.DB)
	methods := make([]string, 0, 2)
	if wx && store.providerEnabled(ctx, string(payment.ProviderWxpay)) {
		methods = append(methods, string(payment.ProviderWxpay))
	}
	if ali && store.providerEnabled(ctx, string(payment.ProviderAlipay)) {
		methods = append(methods, string(payment.ProviderAlipay))
	}
	respondOK(c, gin.H{"methods": methods})
}
