package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

type SubscriptionCreemPayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestCreemPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionCreemPayRequest

	// Keep body for debugging consistency (like RequestCreemPay)
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅支付请求读取失败 error_type=%T", err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "read query error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
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
	if plan.CreemProductId == "" {
		common.ApiErrorMsg(c, "该套餐未配置 CreemProductId")
		return
	}
	// Subscription fulfillment always requires an authenticated callback. Test
	// mode changes the upstream endpoint, but must not turn signature checking
	// into an optional part of the payment contract.
	if setting.CreemWebhookSecret == "" {
		common.ApiErrorMsg(c, "Creem Webhook 未配置")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}

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

	reference := "sub-creem-ref-" + randstr.String(6)
	referenceId := "sub_ref_" + common.Sha1([]byte(reference+time.Now().String()+user.Username))

	// create pending order first
	order, err := model.CreatePendingSubscriptionOrder(userId, plan.Id, referenceId, model.PaymentMethodCreem, model.PaymentProviderCreem, model.SubscriptionCheckoutPolicy{
		Currency:         "USD",
		CurrencySource:   model.SubscriptionCurrencySourceProviderCallback,
		AmountMultiplier: "1",
		CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// Reuse Creem checkout generator by building a lightweight product reference.
	product := &CreemProduct{
		ProductId: order.ExpectedProductId,
		Name:      plan.Title,
		Price:     plan.PriceAmount,
		Currency:  order.ExpectedCurrency,
		Quota:     0,
	}

	checkoutUrl, checkoutId, err := genCreemLink(c.Request.Context(), referenceId, product, user.Email, user.Username, map[string]string{
		"new_api_subscription_product_id":    order.ExpectedProductId,
		"new_api_subscription_snapshot_hash": order.SnapshotHash,
	})
	if err != nil {
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderCreem); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅订单过期标记失败 trade_no=%s error_type=%T", referenceId, expireErr))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅支付链接创建失败 trade_no=%s product_id=%s error_type=%T", referenceId, product.ProductId, err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	if err := model.SetSubscriptionOrderCheckoutId(referenceId, model.PaymentProviderCreem, checkoutId); err != nil {
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderCreem); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅订单过期标记失败 trade_no=%s error_type=%T", referenceId, expireErr))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem checkout 标识保存失败 trade_no=%s error_type=%T", referenceId, err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutUrl,
			"order_id":     referenceId,
		},
	})
}
