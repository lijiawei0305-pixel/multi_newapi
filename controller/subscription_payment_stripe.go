package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/thanhpk/randstr"
)

type SubscriptionStripePayRequest struct {
	PlanId int `json:"plan_id"`
}

const (
	stripeSubscriptionProductMetadataKey  = "new_api_subscription_product_id"
	stripeSubscriptionSnapshotMetadataKey = "new_api_subscription_snapshot_hash"
)

func SubscriptionRequestStripePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionStripePayRequest
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
	if plan.StripePriceId == "" {
		common.ApiErrorMsg(c, "该套餐未配置 StripePriceId")
		return
	}
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		common.ApiErrorMsg(c, "Stripe 未配置或密钥无效")
		return
	}
	if setting.StripeWebhookSecret == "" {
		common.ApiErrorMsg(c, "Stripe Webhook 未配置")
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

	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "sub_ref_" + common.Sha1([]byte(reference))

	order, err := model.CreatePendingSubscriptionOrder(
		userId,
		plan.Id,
		referenceId,
		model.PaymentMethodStripe,
		model.PaymentProviderStripe,
		model.SubscriptionCheckoutPolicy{
			Currency:         strings.ToUpper(strings.TrimSpace(plan.Currency)),
			CurrencySource:   model.SubscriptionCurrencySourceProviderCallback,
			AmountMultiplier: "1",
			CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
		},
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建订阅订单失败 user_id=%d plan_id=%d trade_no=%s error_type=%T", userId, plan.Id, referenceId, err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	payLink, checkoutId, err := genStripeSubscriptionLink(
		referenceId,
		user.StripeCustomer,
		user.Email,
		order.ExpectedProductId,
		order.SnapshotHash,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 订阅支付链接创建失败 trade_no=%s plan_id=%d error_type=%T", referenceId, plan.Id, err))
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderStripe); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 订阅支付链接创建失败后关闭本地订单失败 trade_no=%s error_type=%T", referenceId, expireErr))
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	if err := model.SetSubscriptionOrderCheckoutId(referenceId, model.PaymentProviderStripe, checkoutId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 保存订阅结账会话失败 trade_no=%s checkout_id=%s error_type=%T", referenceId, checkoutId, err))
		if _, expireErr := session.Expire(checkoutId, nil); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 关闭未绑定结账会话失败 trade_no=%s checkout_id=%s error_type=%T", referenceId, checkoutId, expireErr))
		}
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderStripe); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 保存结账会话失败后关闭本地订单失败 trade_no=%s error_type=%T", referenceId, expireErr))
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": payLink,
			"order_id": referenceId,
		},
	})
}

func genStripeSubscriptionLink(referenceId string, customerId string, email string, priceId string, snapshotHash string) (string, string, error) {
	stripe.Key = setting.StripeApiSecret

	params := newStripeSubscriptionSessionParams(referenceId, customerId, email, priceId, snapshotHash)
	result, err := session.New(params)
	if err != nil {
		return "", "", err
	}
	if result == nil || strings.TrimSpace(result.ID) == "" || strings.TrimSpace(result.URL) == "" {
		return "", "", fmt.Errorf("Stripe returned an incomplete Checkout Session")
	}
	return result.URL, result.ID, nil
}

func newStripeSubscriptionSessionParams(referenceId string, customerId string, email string, priceId string, snapshotHash string) *stripe.CheckoutSessionParams {
	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(paymentReturnPath("/console/topup")),
		CancelURL:         stripe.String(paymentReturnPath("/console/topup")),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceId),
				Quantity: stripe.Int64(1),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
	}
	params.AddMetadata(stripeSubscriptionProductMetadataKey, priceId)
	params.AddMetadata(stripeSubscriptionSnapshotMetadataKey, snapshotHash)

	if customerId == "" {
		if email != "" {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}
	return params
}
