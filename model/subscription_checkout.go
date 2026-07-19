package model

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SubscriptionOrderSnapshotVersion = 1

	SubscriptionCheckoutModeOneTime = "one_time"
	SubscriptionAmountPolicyV1      = "decimal_round_2_v1"

	SubscriptionCurrencySourceProviderCallback = "provider_callback"
	SubscriptionCurrencySourceMerchantContract = "merchant_contract"

	SubscriptionOrderStatusReconciliationRequired = "reconciliation_required"
	SubscriptionOrderReviewRequired               = "required"
	SubscriptionOrderReviewResolved               = "resolved"

	SubscriptionReceiptDispositionFulfilled          = "fulfilled"
	SubscriptionReceiptDispositionReconciliation     = "reconciliation"
	SubscriptionReceiptDispositionDuplicatePayment   = "duplicate_payment"
	SubscriptionReceiptDispositionReviewClosed       = "review_closed"
	SubscriptionReceiptDispositionExternallyRefunded = "externally_refunded"
)

var (
	ErrSubscriptionOrderSnapshotMissing        = errors.New("subscription order snapshot missing")
	ErrSubscriptionPaymentFactMismatch         = errors.New("subscription payment fact mismatch")
	ErrSubscriptionPaymentReceiptConflict      = errors.New("subscription payment receipt conflict")
	ErrSubscriptionOrderReconciliationRequired = errors.New("subscription order requires reconciliation")
	ErrSubscriptionPurchaseLimitReached        = errors.New("subscription purchase limit reached")
	ErrSubscriptionProviderCheckoutIDMismatch  = errors.New("subscription provider checkout id mismatch")
	ErrSubscriptionReviewAlreadyResolved       = errors.New("subscription payment review already resolved")
	ErrSubscriptionReviewActionInvalid         = errors.New("subscription payment review action invalid")
)

// SubscriptionCheckoutPolicy describes the provider-side amount contract used
// to create a checkout. AmountMultiplier is applied to the plan's USD price at
// order creation (for example, Epay's configured CNY-per-USD rate). The result
// is frozen as a two-decimal major-unit string before any network request.
type SubscriptionCheckoutPolicy struct {
	Currency         string
	CurrencySource   string
	AmountMultiplier string
	CheckoutMode     string
}

type SubscriptionPlanSnapshotV1 struct {
	PlanId                  int    `json:"plan_id"`
	PlanUpdatedAt           int64  `json:"plan_updated_at"`
	Title                   string `json:"title"`
	PriceAmount             string `json:"price_amount"`
	Currency                string `json:"currency"`
	DurationUnit            string `json:"duration_unit"`
	DurationValue           int    `json:"duration_value"`
	CustomSeconds           int64  `json:"custom_seconds"`
	TotalAmount             int64  `json:"total_amount"`
	QuotaResetPeriod        string `json:"quota_reset_period"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds"`
	MaxPurchasePerUser      int    `json:"max_purchase_per_user"`
	UpgradeGroup            string `json:"upgrade_group"`
	DowngradeGroup          string `json:"downgrade_group"`
	AllowWalletOverflow     bool   `json:"allow_wallet_overflow"`
}

type SubscriptionPaymentExpectationSnapshotV1 struct {
	Provider         string `json:"provider"`
	PaymentMethod    string `json:"payment_method"`
	Amount           string `json:"amount"`
	AmountMultiplier string `json:"amount_multiplier"`
	AmountPolicy     string `json:"amount_policy"`
	Currency         string `json:"currency"`
	CurrencySource   string `json:"currency_source"`
	ProductId        string `json:"product_id"`
	CheckoutMode     string `json:"checkout_mode"`
}

type SubscriptionCheckoutSnapshotV1 struct {
	Version     int                                      `json:"version"`
	Plan        SubscriptionPlanSnapshotV1               `json:"plan"`
	Expectation SubscriptionPaymentExpectationSnapshotV1 `json:"expectation"`
}

// VerifiedSubscriptionPaymentFact is constructed only after a provider adapter
// has authenticated a paid callback. It intentionally carries canonical facts
// and a payload hash, never the provider's raw PII-bearing body.
type VerifiedSubscriptionPaymentFact struct {
	TradeNo               string `json:"trade_no"`
	Provider              string `json:"provider"`
	ProviderEventId       string `json:"provider_event_id"`
	ProviderTransactionId string `json:"provider_transaction_id"`
	ProviderCheckoutId    string `json:"provider_checkout_id"`
	Amount                string `json:"amount"`
	PaidAmount            string `json:"paid_amount"`
	Currency              string `json:"currency"`
	CurrencySource        string `json:"currency_source"`
	ProductId             string `json:"product_id"`
	PaymentMethod         string `json:"payment_method"`
	CheckoutMode          string `json:"checkout_mode"`
	SnapshotHash          string `json:"snapshot_hash"`
	PayloadHash           string `json:"payload_hash"`
	PaidAt                int64  `json:"paid_at"`
}

// SubscriptionPaymentReceipt is the durable provider-transaction idempotency
// boundary. The same provider transaction cannot fulfill two local orders on
// SQLite, MySQL, or PostgreSQL.
type SubscriptionPaymentReceipt struct {
	Id int `json:"id"`

	OrderId    int    `json:"order_id" gorm:"not null;index:idx_subscription_payment_receipts_order_lookup"`
	ReceiptKey string `json:"receipt_key" gorm:"type:varchar(191);not null;uniqueIndex"`
	ClaimId    string `json:"claim_id" gorm:"type:varchar(64)"`

	Provider              string `json:"provider" gorm:"type:varchar(50);not null"`
	ProviderTransactionId string `json:"provider_transaction_id" gorm:"type:varchar(128);not null"`
	ProviderEventId       string `json:"provider_event_id" gorm:"type:varchar(128)"`
	ProviderCheckoutId    string `json:"provider_checkout_id" gorm:"type:varchar(128)"`
	Amount                string `json:"amount" gorm:"type:varchar(32);not null"`
	PaidAmount            string `json:"paid_amount" gorm:"type:varchar(32);not null"`
	Currency              string `json:"currency" gorm:"type:varchar(8);not null"`
	CurrencySource        string `json:"currency_source" gorm:"type:varchar(32);not null"`
	ProductId             string `json:"product_id" gorm:"type:varchar(191);not null"`
	PaymentMethod         string `json:"payment_method" gorm:"type:varchar(50);not null"`
	CheckoutMode          string `json:"checkout_mode" gorm:"type:varchar(32);not null"`
	SnapshotHash          string `json:"snapshot_hash" gorm:"type:varchar(64)"`
	PayloadHash           string `json:"payload_hash" gorm:"type:varchar(64)"`
	Disposition           string `json:"disposition" gorm:"type:varchar(32)"`
	Reason                string `json:"reason" gorm:"type:text"`
	PaidAt                int64  `json:"paid_at" gorm:"type:bigint;not null"`
	CreatedAt             int64  `json:"created_at" gorm:"type:bigint;not null"`
}

// SubscriptionPaymentEvidence records every authenticated callback separately
// from the provider-transaction receipt. This preserves conflicting events
// without weakening the one-provider-transaction/one-order invariant.
type SubscriptionPaymentEvidence struct {
	Id int `json:"id"`

	OrderId                int    `json:"order_id" gorm:"not null;index"`
	EvidenceKey            string `json:"evidence_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	Provider               string `json:"provider" gorm:"type:varchar(50);not null"`
	ProviderTransactionKey string `json:"provider_transaction_key" gorm:"type:varchar(64);index"`
	ProviderEventKey       string `json:"provider_event_key" gorm:"type:varchar(64);index"`
	PayloadHash            string `json:"payload_hash" gorm:"type:varchar(64);index"`
	FactStatus             string `json:"fact_status" gorm:"type:varchar(16);not null"`
	CanonicalFact          string `json:"canonical_fact" gorm:"type:text;not null"`
	Reason                 string `json:"reason" gorm:"type:text"`
	CreatedAt              int64  `json:"created_at" gorm:"type:bigint;not null"`
}

type SubscriptionPaymentReviewDecision struct {
	Id int `json:"id"`

	OrderId     int    `json:"order_id" gorm:"not null;index"`
	DecisionKey string `json:"decision_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	ReceiptId   int    `json:"receipt_id" gorm:"not null;index"`
	Action      string `json:"action" gorm:"type:varchar(32);not null"`
	Note        string `json:"note" gorm:"type:text;not null"`
	ActorId     int    `json:"actor_id" gorm:"not null;index"`
	ReviewTime  int64  `json:"review_time" gorm:"type:bigint;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint;not null"`
}

func normalizeSubscriptionCheckoutMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "one_time", "onetime", "payment":
		return SubscriptionCheckoutModeOneTime, nil
	default:
		return "", fmt.Errorf("unsupported subscription checkout mode %q", mode)
	}
}

func normalizeSubscriptionCurrency(currency string) (string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return "", fmt.Errorf("invalid subscription payment currency %q", currency)
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return "", fmt.Errorf("invalid subscription payment currency %q", currency)
		}
	}
	return currency, nil
}

func normalizeSubscriptionCurrencySource(source string) (string, error) {
	source = strings.TrimSpace(source)
	switch source {
	case SubscriptionCurrencySourceProviderCallback, SubscriptionCurrencySourceMerchantContract:
		return source, nil
	default:
		return "", fmt.Errorf("invalid subscription currency source %q", source)
	}
}

func normalizeSubscriptionMajorAmount(amount string) (string, decimal.Decimal, error) {
	amount = strings.TrimSpace(amount)
	value, err := decimal.NewFromString(amount)
	if err != nil || value.IsNegative() {
		return "", decimal.Zero, fmt.Errorf("invalid subscription payment amount %q", amount)
	}
	rounded := value.Round(2)
	if !value.Equal(rounded) {
		return "", decimal.Zero, fmt.Errorf("subscription payment amount has more than two decimals: %q", amount)
	}
	canonical := rounded.StringFixed(2)
	if len(canonical) > 32 {
		return "", decimal.Zero, errors.New("subscription payment amount is too large")
	}
	return canonical, rounded, nil
}

func subscriptionExpectedProductId(plan *SubscriptionPlan, provider string) (string, error) {
	if plan == nil || plan.Id <= 0 {
		return "", errors.New("invalid subscription plan")
	}
	var productId string
	switch provider {
	case PaymentProviderStripe:
		productId = plan.StripePriceId
	case PaymentProviderCreem:
		productId = plan.CreemProductId
	case PaymentProviderWaffoPancake:
		productId = plan.WaffoPancakeProductId
	case PaymentProviderEpay, PaymentProviderBalance:
		productId = fmt.Sprintf("plan:%d", plan.Id)
	default:
		return "", fmt.Errorf("unsupported subscription payment provider %q", provider)
	}
	productId = strings.TrimSpace(productId)
	if productId == "" || len(productId) > 191 {
		return "", errors.New("subscription payment product is missing or too long")
	}
	return productId, nil
}

func buildSubscriptionCheckoutSnapshot(plan *SubscriptionPlan, paymentMethod string, provider string, policy SubscriptionCheckoutPolicy) (SubscriptionCheckoutSnapshotV1, []byte, string, error) {
	if plan == nil || plan.Id <= 0 {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("invalid subscription plan")
	}
	if math.IsNaN(plan.PriceAmount) || math.IsInf(plan.PriceAmount, 0) || plan.PriceAmount < 0 {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("invalid subscription plan price")
	}
	basePrice := decimal.NewFromFloat(plan.PriceAmount)
	if !basePrice.Equal(basePrice.Round(6)) {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("subscription plan price must have at most six decimals")
	}
	multiplierText := strings.TrimSpace(policy.AmountMultiplier)
	if multiplierText == "" {
		multiplierText = "1"
	}
	multiplier, err := decimal.NewFromString(multiplierText)
	if err != nil || !multiplier.IsPositive() {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("invalid subscription payment amount multiplier")
	}
	expectedAmount := basePrice.Mul(multiplier).Round(2)
	if basePrice.IsPositive() && expectedAmount.IsZero() {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("subscription plan price rounds to zero for payment")
	}
	expectedAmountText, _, err := normalizeSubscriptionMajorAmount(expectedAmount.StringFixed(2))
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	planPriceText := basePrice.Round(6).StringFixed(6)
	multiplierText = multiplier.String()
	currency, err := normalizeSubscriptionCurrency(policy.Currency)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	currencySource, err := normalizeSubscriptionCurrencySource(policy.CurrencySource)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	checkoutMode, err := normalizeSubscriptionCheckoutMode(policy.CheckoutMode)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	productId, err := subscriptionExpectedProductId(plan, provider)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	planCurrency, err := normalizeSubscriptionCurrency(plan.Currency)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	if _, err := calcPlanEndTime(timeForSubscriptionSnapshotValidation(), plan); err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	if plan.TotalAmount < 0 || plan.MaxPurchasePerUser < 0 {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("invalid subscription entitlement limits")
	}
	resetPeriod := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if resetPeriod == SubscriptionResetCustom && plan.QuotaResetCustomSeconds <= 0 {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", errors.New("invalid custom subscription reset period")
	}
	allowWalletOverflow := true
	if plan.AllowWalletOverflow != nil {
		allowWalletOverflow = *plan.AllowWalletOverflow
	}
	snapshot := SubscriptionCheckoutSnapshotV1{
		Version: SubscriptionOrderSnapshotVersion,
		Plan: SubscriptionPlanSnapshotV1{
			PlanId:                  plan.Id,
			PlanUpdatedAt:           plan.UpdatedAt,
			Title:                   plan.Title,
			PriceAmount:             planPriceText,
			Currency:                planCurrency,
			DurationUnit:            plan.DurationUnit,
			DurationValue:           plan.DurationValue,
			CustomSeconds:           plan.CustomSeconds,
			TotalAmount:             plan.TotalAmount,
			QuotaResetPeriod:        resetPeriod,
			QuotaResetCustomSeconds: plan.QuotaResetCustomSeconds,
			MaxPurchasePerUser:      plan.MaxPurchasePerUser,
			UpgradeGroup:            strings.TrimSpace(plan.UpgradeGroup),
			DowngradeGroup:          strings.TrimSpace(plan.DowngradeGroup),
			AllowWalletOverflow:     allowWalletOverflow,
		},
		Expectation: SubscriptionPaymentExpectationSnapshotV1{
			Provider:         provider,
			PaymentMethod:    paymentMethod,
			Amount:           expectedAmountText,
			AmountMultiplier: multiplierText,
			AmountPolicy:     SubscriptionAmountPolicyV1,
			Currency:         currency,
			CurrencySource:   currencySource,
			ProductId:        productId,
			CheckoutMode:     checkoutMode,
		},
	}
	encoded, err := common.Marshal(snapshot)
	if err != nil {
		return SubscriptionCheckoutSnapshotV1{}, nil, "", err
	}
	hash := fmt.Sprintf("%x", common.Sha256Raw(encoded))
	return snapshot, encoded, hash, nil
}

// timeForSubscriptionSnapshotValidation is deliberately fixed: duration
// validation only depends on the plan fields, while a stable instant keeps the
// snapshot builder deterministic and free of checkout-time entitlement dates.
func timeForSubscriptionSnapshotValidation() time.Time {
	return time.Unix(1_700_000_000, 0)
}

// CreatePendingSubscriptionOrder freezes the current plan and payment contract
// in one DB transaction. Callers must commit this local intent before creating a
// remote checkout session.
func newSubscriptionOrderFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, tradeNo string, paymentMethod string, provider string, policy SubscriptionCheckoutPolicy) (*SubscriptionOrder, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	snapshot, encoded, snapshotHash, err := buildSubscriptionCheckoutSnapshot(plan, paymentMethod, provider, policy)
	if err != nil {
		return nil, err
	}
	return &SubscriptionOrder{
		UserId:            userId,
		PlanId:            plan.Id,
		Money:             plan.PriceAmount,
		TradeNo:           tradeNo,
		PaymentMethod:     paymentMethod,
		PaymentProvider:   provider,
		Status:            common.TopUpStatusPending,
		CreateTime:        getDBTimestampTx(tx),
		SnapshotVersion:   SubscriptionOrderSnapshotVersion,
		CheckoutSnapshot:  string(encoded),
		SnapshotHash:      snapshotHash,
		ExpectedAmount:    snapshot.Expectation.Amount,
		ExpectedCurrency:  snapshot.Expectation.Currency,
		ExpectedProductId: snapshot.Expectation.ProductId,
		ExpectedMode:      snapshot.Expectation.CheckoutMode,
		CurrencySource:    snapshot.Expectation.CurrencySource,
	}, nil
}

func CreatePendingSubscriptionOrder(userId int, planId int, tradeNo string, paymentMethod string, provider string, policy SubscriptionCheckoutPolicy) (*SubscriptionOrder, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentMethod = strings.TrimSpace(paymentMethod)
	provider = strings.TrimSpace(provider)
	if userId <= 0 || planId <= 0 || tradeNo == "" || len(tradeNo) > 255 ||
		paymentMethod == "" || len(paymentMethod) > 50 || provider == "" || len(provider) > 50 {
		return nil, errors.New("invalid subscription checkout order")
	}
	var order *SubscriptionOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		plan, err := getSubscriptionPlanByIdForUpdateTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("套餐未启用")
		}
		if plan.MaxPurchasePerUser > 0 {
			var count int64
			if err := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND plan_id = ?", userId, plan.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= int64(plan.MaxPurchasePerUser) {
				return ErrSubscriptionPurchaseLimitReached
			}
		}
		order, err = newSubscriptionOrderFromPlanTx(tx, userId, plan, tradeNo, paymentMethod, provider, policy)
		if err != nil {
			return err
		}
		return tx.Create(order).Error
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

func decodeSubscriptionCheckoutSnapshot(order *SubscriptionOrder) (*SubscriptionCheckoutSnapshotV1, error) {
	if order == nil || order.SnapshotVersion != SubscriptionOrderSnapshotVersion || strings.TrimSpace(order.CheckoutSnapshot) == "" {
		return nil, ErrSubscriptionOrderSnapshotMissing
	}
	hash := fmt.Sprintf("%x", common.Sha256Raw([]byte(order.CheckoutSnapshot)))
	if !strings.EqualFold(hash, strings.TrimSpace(order.SnapshotHash)) {
		return nil, fmt.Errorf("%w: checkout snapshot hash mismatch", ErrSubscriptionPaymentFactMismatch)
	}
	var snapshot SubscriptionCheckoutSnapshotV1
	if err := common.UnmarshalJsonStr(order.CheckoutSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: invalid checkout snapshot", ErrSubscriptionPaymentFactMismatch)
	}
	if snapshot.Version != SubscriptionOrderSnapshotVersion || snapshot.Plan.PlanId != order.PlanId {
		return nil, fmt.Errorf("%w: checkout snapshot identity mismatch", ErrSubscriptionPaymentFactMismatch)
	}
	planPrice, err := decimal.NewFromString(snapshot.Plan.PriceAmount)
	if err != nil || planPrice.IsNegative() || !planPrice.Equal(planPrice.Round(6)) {
		return nil, fmt.Errorf("%w: checkout snapshot plan price is invalid", ErrSubscriptionPaymentFactMismatch)
	}
	multiplier, err := decimal.NewFromString(snapshot.Expectation.AmountMultiplier)
	if err != nil || !multiplier.IsPositive() || multiplier.String() != snapshot.Expectation.AmountMultiplier ||
		snapshot.Expectation.AmountPolicy != SubscriptionAmountPolicyV1 {
		return nil, fmt.Errorf("%w: checkout snapshot amount policy is invalid", ErrSubscriptionPaymentFactMismatch)
	}
	expectedAmount, _, err := normalizeSubscriptionMajorAmount(snapshot.Expectation.Amount)
	if err != nil || expectedAmount != planPrice.Mul(multiplier).Round(2).StringFixed(2) {
		return nil, fmt.Errorf("%w: checkout snapshot amount derivation is invalid", ErrSubscriptionPaymentFactMismatch)
	}
	if snapshot.Expectation.Provider != order.PaymentProvider || snapshot.Expectation.PaymentMethod != order.PaymentMethod ||
		snapshot.Expectation.Amount != order.ExpectedAmount || snapshot.Expectation.Currency != order.ExpectedCurrency ||
		snapshot.Expectation.ProductId != order.ExpectedProductId || snapshot.Expectation.CheckoutMode != order.ExpectedMode ||
		snapshot.Expectation.CurrencySource != order.CurrencySource {
		return nil, fmt.Errorf("%w: duplicated checkout expectation mismatch", ErrSubscriptionPaymentFactMismatch)
	}
	return &snapshot, nil
}

func (snapshot *SubscriptionCheckoutSnapshotV1) planForFulfillment() (*SubscriptionPlan, error) {
	if snapshot == nil {
		return nil, ErrSubscriptionOrderSnapshotMissing
	}
	price, err := decimal.NewFromString(snapshot.Plan.PriceAmount)
	if err != nil || price.IsNegative() || !price.Equal(price.Round(6)) {
		return nil, fmt.Errorf("%w: invalid snapshotted plan price", ErrSubscriptionPaymentFactMismatch)
	}
	allowWalletOverflow := snapshot.Plan.AllowWalletOverflow
	plan := &SubscriptionPlan{
		Id:                      snapshot.Plan.PlanId,
		Title:                   snapshot.Plan.Title,
		PriceAmount:             price.InexactFloat64(),
		Currency:                snapshot.Plan.Currency,
		DurationUnit:            snapshot.Plan.DurationUnit,
		DurationValue:           snapshot.Plan.DurationValue,
		CustomSeconds:           snapshot.Plan.CustomSeconds,
		MaxPurchasePerUser:      snapshot.Plan.MaxPurchasePerUser,
		UpgradeGroup:            snapshot.Plan.UpgradeGroup,
		DowngradeGroup:          snapshot.Plan.DowngradeGroup,
		TotalAmount:             snapshot.Plan.TotalAmount,
		QuotaResetPeriod:        snapshot.Plan.QuotaResetPeriod,
		QuotaResetCustomSeconds: snapshot.Plan.QuotaResetCustomSeconds,
		AllowWalletOverflow:     &allowWalletOverflow,
	}
	if _, err := calcPlanEndTime(timeForSubscriptionSnapshotValidation(), plan); err != nil {
		return nil, fmt.Errorf("%w: invalid snapshotted entitlement duration", ErrSubscriptionPaymentFactMismatch)
	}
	if plan.TotalAmount < 0 || plan.MaxPurchasePerUser < 0 ||
		(NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetCustom && plan.QuotaResetCustomSeconds <= 0) {
		return nil, fmt.Errorf("%w: invalid snapshotted entitlement policy", ErrSubscriptionPaymentFactMismatch)
	}
	return plan, nil
}

func normalizeVerifiedSubscriptionPaymentFact(fact VerifiedSubscriptionPaymentFact) (VerifiedSubscriptionPaymentFact, error) {
	fact.TradeNo = strings.TrimSpace(fact.TradeNo)
	fact.Provider = strings.TrimSpace(fact.Provider)
	fact.ProviderEventId = strings.TrimSpace(fact.ProviderEventId)
	fact.ProviderTransactionId = strings.TrimSpace(fact.ProviderTransactionId)
	fact.ProviderCheckoutId = strings.TrimSpace(fact.ProviderCheckoutId)
	fact.ProductId = strings.TrimSpace(fact.ProductId)
	fact.PaymentMethod = strings.TrimSpace(fact.PaymentMethod)
	fact.SnapshotHash = strings.ToLower(strings.TrimSpace(fact.SnapshotHash))
	fact.PayloadHash = strings.ToLower(strings.TrimSpace(fact.PayloadHash))
	if fact.TradeNo == "" || len(fact.TradeNo) > 255 || fact.Provider == "" || len(fact.Provider) > 50 ||
		fact.PaymentMethod == "" || len(fact.PaymentMethod) > 50 {
		return fact, errors.New("invalid subscription payment fact identity")
	}
	if fact.ProviderTransactionId == "" || len(fact.ProviderTransactionId) > 128 || len(fact.ProviderEventId) > 128 || len(fact.ProviderCheckoutId) > 128 {
		return fact, errors.New("invalid subscription provider transaction identity")
	}
	if fact.ProductId == "" || len(fact.ProductId) > 191 {
		return fact, errors.New("invalid subscription provider product")
	}
	amount, _, err := normalizeSubscriptionMajorAmount(fact.Amount)
	if err != nil {
		return fact, err
	}
	fact.Amount = amount
	if strings.TrimSpace(fact.PaidAmount) == "" {
		return fact, errors.New("subscription paid amount is missing")
	}
	paidAmount, _, err := normalizeSubscriptionMajorAmount(fact.PaidAmount)
	if err != nil {
		return fact, err
	}
	fact.PaidAmount = paidAmount
	fact.Currency, err = normalizeSubscriptionCurrency(fact.Currency)
	if err != nil {
		return fact, err
	}
	fact.CurrencySource, err = normalizeSubscriptionCurrencySource(fact.CurrencySource)
	if err != nil {
		return fact, err
	}
	fact.CheckoutMode, err = normalizeSubscriptionCheckoutMode(fact.CheckoutMode)
	if err != nil {
		return fact, err
	}
	if fact.SnapshotHash != "" && !isSHA256Hex(fact.SnapshotHash) {
		return fact, errors.New("invalid subscription snapshot hash")
	}
	if !isSHA256Hex(fact.PayloadHash) {
		return fact, errors.New("invalid subscription provider payload hash")
	}
	return fact, nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func boundedSubscriptionEvidenceValue(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxLength {
		return value
	}
	hash := fmt.Sprintf("%x", common.Sha256Raw([]byte(value)))
	if maxLength >= len(hash) {
		return hash
	}
	if maxLength <= 2 {
		return hash[:maxLength]
	}
	return "h:" + hash[:maxLength-2]
}

func boundedSubscriptionReviewReason(cause error) string {
	if cause == nil {
		return "subscription payment review requested"
	}
	reason := strings.TrimSpace(cause.Error())
	const maxReasonLength = 2000
	if len(reason) <= maxReasonLength {
		return reason
	}
	hash := fmt.Sprintf("%x", common.Sha256Raw([]byte(reason)))
	return reason[:maxReasonLength-len(hash)-12] + " [sha256:" + hash + "]"
}

func sanitizeSubscriptionPaymentFactForStorage(fact VerifiedSubscriptionPaymentFact) VerifiedSubscriptionPaymentFact {
	fact.TradeNo = boundedSubscriptionEvidenceValue(fact.TradeNo, 255)
	fact.Provider = boundedSubscriptionEvidenceValue(fact.Provider, 50)
	fact.ProviderEventId = boundedSubscriptionEvidenceValue(fact.ProviderEventId, 128)
	fact.ProviderTransactionId = boundedSubscriptionEvidenceValue(fact.ProviderTransactionId, 128)
	fact.ProviderCheckoutId = boundedSubscriptionEvidenceValue(fact.ProviderCheckoutId, 128)
	fact.Amount = boundedSubscriptionEvidenceValue(fact.Amount, 32)
	fact.PaidAmount = boundedSubscriptionEvidenceValue(fact.PaidAmount, 32)
	fact.Currency = boundedSubscriptionEvidenceValue(fact.Currency, 8)
	fact.CurrencySource = boundedSubscriptionEvidenceValue(fact.CurrencySource, 32)
	fact.ProductId = boundedSubscriptionEvidenceValue(fact.ProductId, 191)
	fact.PaymentMethod = boundedSubscriptionEvidenceValue(fact.PaymentMethod, 50)
	fact.CheckoutMode = boundedSubscriptionEvidenceValue(fact.CheckoutMode, 32)
	fact.SnapshotHash = boundedSubscriptionEvidenceValue(fact.SnapshotHash, 64)
	fact.PayloadHash = boundedSubscriptionEvidenceValue(fact.PayloadHash, 64)
	return fact
}

func subscriptionEvidenceIdentityHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%x", common.Sha256Raw([]byte(value)))
}

func recordSubscriptionPaymentEvidenceTx(tx *gorm.DB, order *SubscriptionOrder, fact VerifiedSubscriptionPaymentFact, factErr error) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription payment evidence args")
	}
	storedFact := sanitizeSubscriptionPaymentFactForStorage(fact)
	canonicalFact, err := canonicalSubscriptionPaymentFact(storedFact)
	if err != nil {
		return err
	}
	factStatus := "valid"
	reason := ""
	if factErr != nil {
		factStatus = "invalid"
		reason = boundedSubscriptionReviewReason(factErr)
	}
	payloadHash := strings.ToLower(strings.TrimSpace(fact.PayloadHash))
	if !isSHA256Hex(payloadHash) {
		payloadHash = subscriptionEvidenceIdentityHash(payloadHash)
	}
	evidenceKeyMaterial := fmt.Sprintf("%d\x00%s\x00%s\x00%s", order.Id, order.PaymentProvider, payloadHash, canonicalFact)
	evidence := &SubscriptionPaymentEvidence{
		OrderId:                order.Id,
		EvidenceKey:            fmt.Sprintf("%x", common.Sha256Raw([]byte(evidenceKeyMaterial))),
		Provider:               order.PaymentProvider,
		ProviderTransactionKey: subscriptionEvidenceIdentityHash(fact.ProviderTransactionId),
		ProviderEventKey:       subscriptionEvidenceIdentityHash(fact.ProviderEventId),
		PayloadHash:            payloadHash,
		FactStatus:             factStatus,
		CanonicalFact:          canonicalFact,
		Reason:                 reason,
		CreatedAt:              common.GetTimestamp(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "evidence_key"}},
		DoNothing: true,
	}).Create(evidence).Error
}

func validateSubscriptionPaymentFact(order *SubscriptionOrder, snapshot *SubscriptionCheckoutSnapshotV1, fact VerifiedSubscriptionPaymentFact) error {
	if order == nil || snapshot == nil {
		return fmt.Errorf("%w: missing checkout state", ErrSubscriptionPaymentFactMismatch)
	}
	if fact.TradeNo != order.TradeNo || fact.Provider != order.PaymentProvider || fact.PaymentMethod != order.PaymentMethod {
		return fmt.Errorf("%w: order identity differs", ErrSubscriptionPaymentFactMismatch)
	}
	if fact.Amount != snapshot.Expectation.Amount || fact.Currency != snapshot.Expectation.Currency ||
		fact.CurrencySource != snapshot.Expectation.CurrencySource || fact.ProductId != snapshot.Expectation.ProductId ||
		fact.CheckoutMode != snapshot.Expectation.CheckoutMode || fact.SnapshotHash != order.SnapshotHash {
		return fmt.Errorf("%w: amount, currency, product, mode, or snapshot differs", ErrSubscriptionPaymentFactMismatch)
	}
	expectedPaidAmount, err := decimal.NewFromString(snapshot.Expectation.Amount)
	if err != nil {
		return fmt.Errorf("%w: expected paid amount is invalid", ErrSubscriptionPaymentFactMismatch)
	}
	paidAmount, err := decimal.NewFromString(fact.PaidAmount)
	if err != nil || paidAmount.LessThan(expectedPaidAmount) {
		return fmt.Errorf("%w: paid amount is below the frozen checkout amount", ErrSubscriptionPaymentFactMismatch)
	}
	if order.ProviderCheckoutId != "" && fact.ProviderCheckoutId != order.ProviderCheckoutId {
		return fmt.Errorf("%w: %w", ErrSubscriptionPaymentFactMismatch, ErrSubscriptionProviderCheckoutIDMismatch)
	}
	return nil
}

func subscriptionReceiptKey(provider string, transactionId string) (string, error) {
	provider = strings.TrimSpace(provider)
	transactionId = strings.TrimSpace(transactionId)
	if provider == "" || transactionId == "" {
		return "", errors.New("invalid subscription payment receipt key")
	}
	// Hash the exact case-sensitive identity so MySQL's common case-insensitive
	// collations have the same idempotency semantics as PostgreSQL and SQLite.
	return fmt.Sprintf("%x", common.Sha256Raw([]byte(provider+"\x00"+transactionId))), nil
}

func subscriptionPaymentReceipt(order *SubscriptionOrder, fact VerifiedSubscriptionPaymentFact, disposition string, reason string) (*SubscriptionPaymentReceipt, error) {
	key, err := subscriptionReceiptKey(fact.Provider, fact.ProviderTransactionId)
	if err != nil {
		return nil, err
	}
	return &SubscriptionPaymentReceipt{
		OrderId:               order.Id,
		ReceiptKey:            key,
		ClaimId:               common.GetUUID(),
		Provider:              fact.Provider,
		ProviderTransactionId: fact.ProviderTransactionId,
		ProviderEventId:       fact.ProviderEventId,
		ProviderCheckoutId:    fact.ProviderCheckoutId,
		Amount:                fact.Amount,
		PaidAmount:            fact.PaidAmount,
		Currency:              fact.Currency,
		CurrencySource:        fact.CurrencySource,
		ProductId:             fact.ProductId,
		PaymentMethod:         fact.PaymentMethod,
		CheckoutMode:          fact.CheckoutMode,
		SnapshotHash:          fact.SnapshotHash,
		PayloadHash:           fact.PayloadHash,
		Disposition:           disposition,
		Reason:                reason,
		PaidAt:                fact.PaidAt,
		CreatedAt:             common.GetTimestamp(),
	}, nil
}

func receiptMatchesFact(receipt *SubscriptionPaymentReceipt, fact VerifiedSubscriptionPaymentFact) bool {
	return receipt != nil && receipt.Provider == fact.Provider &&
		receipt.ProviderTransactionId == fact.ProviderTransactionId &&
		receipt.ProviderCheckoutId == fact.ProviderCheckoutId && receipt.Amount == fact.Amount &&
		receipt.PaidAmount == fact.PaidAmount && receipt.Currency == fact.Currency &&
		receipt.CurrencySource == fact.CurrencySource && receipt.ProductId == fact.ProductId &&
		receipt.PaymentMethod == fact.PaymentMethod && receipt.CheckoutMode == fact.CheckoutMode &&
		receipt.SnapshotHash == fact.SnapshotHash
}

func findSubscriptionReceiptByKeyTx(tx *gorm.DB, provider string, transactionId string) (*SubscriptionPaymentReceipt, error) {
	key, err := subscriptionReceiptKey(provider, transactionId)
	if err != nil {
		return nil, err
	}
	var receipt SubscriptionPaymentReceipt
	query := tx.Where("receipt_key = ?", key).Limit(1).Find(&receipt)
	if query.Error != nil {
		return nil, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, nil
	}
	return &receipt, nil
}

func ensureSubscriptionReceiptTx(tx *gorm.DB, order *SubscriptionOrder, fact VerifiedSubscriptionPaymentFact, disposition string, reason string) (*SubscriptionPaymentReceipt, bool, error) {
	receipt, err := subscriptionPaymentReceipt(order, fact, disposition, reason)
	if err != nil {
		return nil, false, err
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "receipt_key"}},
		DoNothing: true,
	}).Create(receipt)
	if result.Error != nil {
		return nil, false, result.Error
	}
	persisted, err := findSubscriptionReceiptByKeyTx(tx, fact.Provider, fact.ProviderTransactionId)
	if err != nil {
		return nil, false, err
	}
	if persisted == nil {
		return nil, false, errors.New("subscription payment receipt insert disappeared")
	}
	if persisted.OrderId != order.Id || !receiptMatchesFact(persisted, fact) {
		return persisted, false, ErrSubscriptionPaymentReceiptConflict
	}
	return persisted, persisted.ClaimId == receipt.ClaimId, nil
}

func canonicalSubscriptionPaymentFact(fact VerifiedSubscriptionPaymentFact) (string, error) {
	encoded, err := common.Marshal(fact)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func persistSubscriptionReviewTx(tx *gorm.DB, order *SubscriptionOrder, fact VerifiedSubscriptionPaymentFact, cause error, disposition string) (error, error) {
	canonicalFact, err := canonicalSubscriptionPaymentFact(fact)
	if err != nil {
		return nil, err
	}
	reviewReason := boundedSubscriptionReviewReason(cause)
	receipt, created, receiptErr := ensureSubscriptionReceiptTx(tx, order, fact, disposition, reviewReason)
	if receiptErr != nil && !errors.Is(receiptErr, ErrSubscriptionPaymentReceiptConflict) {
		return nil, receiptErr
	}
	if receiptErr == nil && !created &&
		(order.Status == common.TopUpStatusSuccess || order.Status == SubscriptionOrderStatusReconciliationRequired) {
		return ErrSubscriptionOrderReconciliationRequired, nil
	}
	if receiptErr != nil {
		cause = errors.Join(cause, receiptErr)
		reviewReason = boundedSubscriptionReviewReason(cause)
	}
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"reconciliation_reason":     reviewReason,
		"review_status":             SubscriptionOrderReviewRequired,
		"review_time":               now,
		"review_related_receipt_id": 0,
		"resolution_action":         "",
		"resolution_note":           "",
		"resolution_receipt_id":     0,
		"resolved_by":               0,
		"resolved_at":               0,
	}
	if receiptErr != nil && errors.Is(receiptErr, ErrSubscriptionPaymentReceiptConflict) && receipt != nil {
		updates["review_related_receipt_id"] = receipt.Id
	}
	if strings.TrimSpace(order.ReviewStatus) == "" {
		updates["first_review_time"] = now
	}
	if strings.TrimSpace(order.ReviewStatus) == "" || order.ReviewStatus == SubscriptionOrderReviewResolved {
		updates["review_previous_status"] = order.Status
		updates["review_previous_complete_time"] = order.CompleteTime
	}
	if order.Status != common.TopUpStatusSuccess {
		// Pending orders move to a dedicated non-fulfillable state. Existing
		// terminal states (failed/expired) remain intact so a late verified
		// payment cannot erase the original outcome or completion timestamp.
		if order.Status == common.TopUpStatusPending {
			updates["status"] = SubscriptionOrderStatusReconciliationRequired
			if order.CompleteTime == 0 {
				updates["complete_time"] = now
			}
		}
		if strings.TrimSpace(order.PaymentFact) == "" {
			updates["payment_fact"] = canonicalFact
		}
	}
	if order.ProviderCheckoutId == "" && fact.ProviderCheckoutId != "" {
		updates["provider_checkout_id"] = fact.ProviderCheckoutId
	}
	if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return fmt.Errorf("%w: %w", ErrSubscriptionOrderReconciliationRequired, cause), nil
}

func persistSubscriptionReconciliationTx(tx *gorm.DB, order *SubscriptionOrder, fact VerifiedSubscriptionPaymentFact, cause error) (error, error) {
	return persistSubscriptionReviewTx(tx, order, fact, cause, SubscriptionReceiptDispositionReconciliation)
}

// SetSubscriptionOrderCheckoutId binds the remote checkout identity without
// allowing it to be replaced by a later or cross-provider value.
func SetSubscriptionOrderCheckoutId(tradeNo string, provider string, checkoutId string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	provider = strings.TrimSpace(provider)
	checkoutId = strings.TrimSpace(checkoutId)
	if tradeNo == "" || provider == "" || checkoutId == "" || len(checkoutId) > 128 {
		return errors.New("invalid subscription provider checkout identity")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionOrderNotFound
			}
			return err
		}
		if order.PaymentProvider != provider {
			return ErrPaymentMethodMismatch
		}
		if order.ProviderCheckoutId == checkoutId {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		if order.ProviderCheckoutId != "" {
			return ErrSubscriptionProviderCheckoutIDMismatch
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND status = ? AND provider_checkout_id = ''", order.Id, common.TopUpStatusPending).
			Update("provider_checkout_id", checkoutId)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		var current SubscriptionOrder
		if err := tx.Select("status", "provider_checkout_id").Where("id = ?", order.Id).First(&current).Error; err != nil {
			return err
		}
		if current.ProviderCheckoutId == checkoutId {
			return nil
		}
		if current.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		return ErrSubscriptionProviderCheckoutIDMismatch
	})
}

// CompleteSubscriptionOrder validates an authenticated provider fact against
// the immutable checkout snapshot, inserts the unique receipt, and grants only
// the snapshotted entitlement. Paid but unverifiable orders are committed to a
// reconciliation state so provider adapters can ACK without granting access.
func CompleteSubscriptionOrder(fact VerifiedSubscriptionPaymentFact) error {
	if strings.TrimSpace(fact.TradeNo) == "" {
		return errors.New("tradeNo is empty")
	}
	var completionResult error
	var logUserId int
	var logPlanTitle string
	var logAmount string
	var logCurrency string
	var logPaymentMethod string
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", strings.TrimSpace(fact.TradeNo)).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionOrderNotFound
			}
			return err
		}
		if strings.TrimSpace(fact.Provider) != order.PaymentProvider {
			return ErrPaymentMethodMismatch
		}
		normalizedFact, factErr := normalizeVerifiedSubscriptionPaymentFact(fact)
		evidenceFact := fact
		if factErr == nil {
			evidenceFact = normalizedFact
		}
		if err := recordSubscriptionPaymentEvidenceTx(tx, &order, evidenceFact, factErr); err != nil {
			return err
		}
		if order.Status == common.TopUpStatusSuccess && order.SnapshotVersion == 0 {
			// Legacy successful orders predate immutable receipts. They have already
			// granted an entitlement, so callbacks remain harmless no-ops.
			return nil
		}
		if factErr != nil {
			// A provider adapter reached this function only after signature and paid
			// status verification. Preserve the structured evidence for manual
			// handling instead of retrying into an unsafe entitlement grant.
			fallback := sanitizeSubscriptionPaymentFactForStorage(fact)
			if fallback.ProviderTransactionId == "" {
				return factErr
			}
			disposition := SubscriptionReceiptDispositionReconciliation
			if order.Status == common.TopUpStatusSuccess {
				disposition = SubscriptionReceiptDispositionDuplicatePayment
			}
			var persistErr error
			completionResult, persistErr = persistSubscriptionReviewTx(tx, &order, fallback, factErr, disposition)
			return persistErr
		}

		if order.Status == common.TopUpStatusSuccess {
			receipt, err := findSubscriptionReceiptByKeyTx(tx, normalizedFact.Provider, normalizedFact.ProviderTransactionId)
			if err != nil {
				return err
			}
			if receipt != nil && receipt.OrderId == order.Id && receiptMatchesFact(receipt, normalizedFact) {
				if receipt.Disposition == SubscriptionReceiptDispositionFulfilled {
					return nil
				}
				completionResult = ErrSubscriptionOrderReconciliationRequired
				return nil
			}
			cause := errors.New("additional verified payment received after subscription fulfillment")
			if snapshot, snapshotErr := decodeSubscriptionCheckoutSnapshot(&order); snapshotErr != nil {
				cause = errors.Join(cause, snapshotErr)
			} else if mismatch := validateSubscriptionPaymentFact(&order, snapshot, normalizedFact); mismatch != nil {
				cause = errors.Join(cause, mismatch)
			}
			var persistErr error
			completionResult, persistErr = persistSubscriptionReviewTx(tx, &order, normalizedFact, cause, SubscriptionReceiptDispositionDuplicatePayment)
			return persistErr
		}
		if order.Status == SubscriptionOrderStatusReconciliationRequired {
			receipt, err := findSubscriptionReceiptByKeyTx(tx, normalizedFact.Provider, normalizedFact.ProviderTransactionId)
			if err != nil {
				return err
			}
			if receipt != nil && receipt.OrderId == order.Id && receiptMatchesFact(receipt, normalizedFact) {
				completionResult = ErrSubscriptionOrderReconciliationRequired
				return nil
			}
			cause := errors.New("additional verified payment evidence received for subscription under reconciliation")
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, cause)
			return persistErr
		}
		if order.Status != common.TopUpStatusPending {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, ErrSubscriptionOrderStatusInvalid)
			return persistErr
		}

		if order.SnapshotVersion == 0 || strings.TrimSpace(order.CheckoutSnapshot) == "" {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, ErrSubscriptionOrderSnapshotMissing)
			return persistErr
		}
		snapshot, snapshotErr := decodeSubscriptionCheckoutSnapshot(&order)
		if snapshotErr != nil {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, snapshotErr)
			return persistErr
		}
		if factMismatch := validateSubscriptionPaymentFact(&order, snapshot, normalizedFact); factMismatch != nil {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, factMismatch)
			return persistErr
		}
		receipt, created, receiptErr := ensureSubscriptionReceiptTx(
			tx,
			&order,
			normalizedFact,
			SubscriptionReceiptDispositionReconciliation,
			"verified payment reserved while entitlement fulfillment is in progress",
		)
		if receiptErr != nil {
			if errors.Is(receiptErr, ErrSubscriptionPaymentReceiptConflict) {
				var persistErr error
				completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, receiptErr)
				return persistErr
			}
			return receiptErr
		}
		if !created {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, errors.New("payment receipt predates pending entitlement fulfillment"))
			return persistErr
		}

		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", order.UserId).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				var persistErr error
				completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, err)
				return persistErr
			}
			return err
		}
		plan, planErr := snapshot.planForFulfillment()
		if planErr != nil {
			var persistErr error
			completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, planErr)
			return persistErr
		}
		_, createErr := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if createErr != nil {
			if errors.Is(createErr, ErrSubscriptionPurchaseLimitReached) {
				var persistErr error
				completionResult, persistErr = persistSubscriptionReconciliationTx(tx, &order, normalizedFact, createErr)
				return persistErr
			}
			return createErr
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		if err := tx.Model(&SubscriptionPaymentReceipt{}).Where("id = ?", receipt.Id).Updates(map[string]interface{}{
			"disposition": SubscriptionReceiptDispositionFulfilled,
			"reason":      "",
		}).Error; err != nil {
			return err
		}
		canonicalFact, err := canonicalSubscriptionPaymentFact(normalizedFact)
		if err != nil {
			return err
		}
		updates := map[string]interface{}{
			"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp(),
			"payment_fact": canonicalFact, "reconciliation_reason": "",
			"review_status": "", "review_time": 0,
		}
		if order.ProviderCheckoutId == "" && normalizedFact.ProviderCheckoutId != "" {
			updates["provider_checkout_id"] = normalizedFact.ProviderCheckoutId
		}
		if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(updates).Error; err != nil {
			return err
		}
		logUserId = order.UserId
		logPlanTitle = snapshot.Plan.Title
		logAmount = order.ExpectedAmount
		logCurrency = order.ExpectedCurrency
		logPaymentMethod = order.PaymentMethod
		upgradeGroup = strings.TrimSpace(snapshot.Plan.UpgradeGroup)
		return nil
	})
	if err != nil {
		return err
	}
	if completionResult != nil {
		return completionResult
	}
	if upgradeGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, upgradeGroup)
	}
	if logUserId > 0 {
		msg := fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %s %s，支付方式: %s", logPlanTitle, logAmount, logCurrency, logPaymentMethod)
		RecordLog(logUserId, LogTypeTopup, msg)
	}
	return nil
}
