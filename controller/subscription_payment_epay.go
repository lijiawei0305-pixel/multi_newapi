package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/epay"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionEpayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

func SubscriptionRequestEpay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionEpayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		common.ApiErrorMsg(c, "支付方式不存在")
		return
	}
	// 官方微信/支付宝走主站进程内真实 SDK（/api/tenant/token-plans/:id/purchase），不得透传给 Epay。
	if operation_setting.IsOfficialPayMethod(req.PaymentMethod) {
		common.ApiErrorMsg(c, "请使用官方微信/支付宝购买入口")
		return
	}

	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	callBackAddress := service.GetCallbackAddress()
	returnUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/return")
	if err != nil {
		common.ApiErrorMsg(c, "回调地址配置错误")
		return
	}
	notifyUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/notify")
	if err != nil {
		common.ApiErrorMsg(c, "回调地址配置错误")
		return
	}

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("SUBUSR%dNO%s", userId, tradeNo)

	client := GetEpayClient()
	if client == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}

	conversionRate := operation_setting.Price
	if math.IsNaN(conversionRate) || math.IsInf(conversionRate, 0) || conversionRate <= 0 {
		common.ApiErrorMsg(c, "支付汇率配置错误")
		return
	}
	order, err := model.CreatePendingSubscriptionOrder(userId, plan.Id, tradeNo, req.PaymentMethod, model.PaymentProviderEpay, model.SubscriptionCheckoutPolicy{
		Currency:         "CNY",
		CurrencySource:   model.SubscriptionCurrencySourceMerchantContract,
		AmountMultiplier: strconv.FormatFloat(conversionRate, 'f', -1, 64),
		CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
	})
	if err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("SUBPLAN:%d:%s", plan.Id, order.SnapshotHash),
		Money:          order.ExpectedAmount,
		Device:         epay.PC,
		NotifyURL:      notifyUrl,
		ReturnURL:      returnUrl,
	})
	if err != nil {
		if expireErr := model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderEpay); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Epay 订阅订单过期标记失败 trade_no=%s error_type=%T", tradeNo, expireErr))
		}
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri})
}

func SubscriptionEpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}

	if len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	client := GetEpayClient()
	if client == nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	fact, err := subscriptionEpayPaymentFact(verifyInfo, params)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epay 订阅回调支付事实无效 trade_no=%s error_type=%T", verifyInfo.ServiceTradeNo, err))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := model.CompleteSubscriptionOrder(fact); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderReconciliationRequired) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epay 订阅订单进入人工核账 trade_no=%s provider_trade_no=%s", verifyInfo.ServiceTradeNo, verifyInfo.TradeNo))
			_, _ = c.Writer.Write([]byte("success"))
			return
		}
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	_, _ = c.Writer.Write([]byte("success"))
}

// SubscriptionEpayReturn handles browser return after payment.
// It verifies the payload and completes the order, then redirects to console.
func SubscriptionEpayReturn(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
		return
	}
	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}

	if len(params) == 0 {
		c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
		return
	}

	client := GetEpayClient()
	if client == nil {
		c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
		return
	}
	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		LockOrder(verifyInfo.ServiceTradeNo)
		defer UnlockOrder(verifyInfo.ServiceTradeNo)
		fact, err := subscriptionEpayPaymentFact(verifyInfo, params)
		if err != nil {
			c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
			return
		}
		if err := model.CompleteSubscriptionOrder(fact); err != nil {
			if errors.Is(err, model.ErrSubscriptionOrderReconciliationRequired) {
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epay 订阅返回进入人工核账 trade_no=%s provider_trade_no=%s", verifyInfo.ServiceTradeNo, verifyInfo.TradeNo))
				c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=pending"))
				return
			}
			c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=fail"))
			return
		}
		c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=success"))
		return
	}
	c.Redirect(http.StatusFound, paymentReturnPath("/console/topup?pay=pending"))
}

func subscriptionEpayPaymentFact(verifyInfo *epay.VerifyResult, params map[string]string) (model.VerifiedSubscriptionPaymentFact, error) {
	if verifyInfo == nil {
		return model.VerifiedSubscriptionPaymentFact{}, errors.New("Epay verification result is missing")
	}
	serviceTradeNo := strings.TrimSpace(verifyInfo.ServiceTradeNo)
	providerTradeNo := strings.TrimSpace(verifyInfo.TradeNo)
	if serviceTradeNo == "" || providerTradeNo == "" {
		return model.VerifiedSubscriptionPaymentFact{}, errors.New("Epay subscription transaction identity is incomplete")
	}

	productId := ""
	snapshotHash := ""
	parts := strings.Split(verifyInfo.Name, ":")
	if len(parts) == 3 && parts[0] == "SUBPLAN" {
		planId, err := strconv.Atoi(parts[1])
		if err == nil && planId > 0 {
			productId = fmt.Sprintf("plan:%d", planId)
		}
		snapshotHash = strings.ToLower(strings.TrimSpace(parts[2]))
	}

	canonicalAmount := strings.TrimSpace(verifyInfo.Money)
	amount, err := decimal.NewFromString(canonicalAmount)
	if err == nil && amount.GreaterThan(decimal.Zero) && amount.Equal(amount.Round(2)) {
		canonicalAmount = amount.StringFixed(2)
	}

	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hasher := sha256.New()
	for _, key := range keys {
		_, _ = hasher.Write([]byte(key))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(params[key]))
		_, _ = hasher.Write([]byte{0})
	}

	return model.VerifiedSubscriptionPaymentFact{
		TradeNo:               serviceTradeNo,
		Provider:              model.PaymentProviderEpay,
		ProviderEventId:       providerTradeNo,
		ProviderTransactionId: providerTradeNo,
		Amount:                canonicalAmount,
		PaidAmount:            canonicalAmount,
		Currency:              "CNY",
		CurrencySource:        model.SubscriptionCurrencySourceMerchantContract,
		ProductId:             productId,
		PaymentMethod:         strings.TrimSpace(verifyInfo.Type),
		SnapshotHash:          snapshotHash,
		PayloadHash:           hex.EncodeToString(hasher.Sum(nil)),
		CheckoutMode:          model.SubscriptionCheckoutModeOneTime,
		// Epay's verified callback does not expose a provider payment timestamp.
		// Keep this unknown; the receipt's CreatedAt records local observation time.
		PaidAt: 0,
	}, nil
}
