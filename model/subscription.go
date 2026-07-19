package model

import (
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

// Subscription duration units
const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"
)

// Subscription quota reset period
const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
)

var (
	ErrSubscriptionOrderNotFound      = errors.New("subscription order not found")
	ErrSubscriptionOrderStatusInvalid = errors.New("subscription order status invalid")
)

// Subscription plan
type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	AllowBalancePay *bool `json:"allow_balance_pay"`

	// Allow falling back to wallet balance after subscription quota is exhausted (empty = true)
	AllowWalletOverflow *bool `json:"allow_wallet_overflow"`

	StripePriceId         string `json:"stripe_price_id" gorm:"type:varchar(128);default:''"`
	CreemProductId        string `json:"creem_product_id" gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id" gorm:"type:varchar(128);default:''"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`

	// Downgrade user group on expiry (empty = revert to the group held before purchase)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *SubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *SubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *SubscriptionPlan) NormalizeDefaults() {
	if p.AllowBalancePay == nil {
		p.AllowBalancePay = common.GetPointer(true)
	}
	if p.AllowWalletOverflow == nil {
		p.AllowWalletOverflow = common.GetPointer(true)
	}
}

// Subscription order (payment -> webhook -> create UserSubscription)
type SubscriptionOrder struct {
	Id     int     `json:"id"`
	UserId int     `json:"user_id" gorm:"index"`
	PlanId int     `json:"plan_id" gorm:"index"`
	Money  float64 `json:"money"`

	TradeNo         string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Status          string `json:"status"`
	CreateTime      int64  `json:"create_time"`
	CompleteTime    int64  `json:"complete_time"`

	// Versioned immutable checkout contract. Provider callbacks are validated
	// against these duplicated indexed fields and the canonical JSON snapshot.
	SnapshotVersion    int    `json:"snapshot_version" gorm:"type:int;not null;default:0"`
	CheckoutSnapshot   string `json:"checkout_snapshot" gorm:"type:text"`
	SnapshotHash       string `json:"snapshot_hash" gorm:"type:varchar(64);index"`
	ExpectedAmount     string `json:"expected_amount" gorm:"type:varchar(32)"`
	ExpectedCurrency   string `json:"expected_currency" gorm:"type:varchar(8)"`
	ExpectedProductId  string `json:"expected_product_id" gorm:"type:varchar(191)"`
	ExpectedMode       string `json:"expected_mode" gorm:"type:varchar(32)"`
	CurrencySource     string `json:"currency_source" gorm:"type:varchar(32)"`
	ProviderCheckoutId string `json:"provider_checkout_id" gorm:"type:varchar(128);index"`

	// PaymentFact contains only normalized, authenticated provider facts. The
	// raw callback body is never persisted because it may contain customer PII.
	PaymentFact                string `json:"payment_fact" gorm:"type:text"`
	ReconciliationReason       string `json:"reconciliation_reason" gorm:"type:text"`
	ReviewStatus               string `json:"review_status" gorm:"type:varchar(32)"`
	FirstReviewTime            int64  `json:"first_review_time" gorm:"type:bigint;not null;default:0"`
	ReviewTime                 int64  `json:"review_time" gorm:"type:bigint;not null;default:0"`
	ReviewPreviousStatus       string `json:"review_previous_status" gorm:"type:varchar(32)"`
	ReviewPreviousCompleteTime int64  `json:"review_previous_complete_time" gorm:"type:bigint;not null;default:0"`
	ReviewRelatedReceiptId     int    `json:"review_related_receipt_id" gorm:"type:int;not null;default:0"`
	ResolutionAction           string `json:"resolution_action" gorm:"type:varchar(32)"`
	ResolutionNote             string `json:"resolution_note" gorm:"type:text"`
	ResolutionReceiptId        int    `json:"resolution_receipt_id" gorm:"type:int;not null;default:0"`
	ResolvedBy                 int    `json:"resolved_by" gorm:"type:int;not null;default:0"`
	ResolvedAt                 int64  `json:"resolved_at" gorm:"type:bigint;not null;default:0"`

	// ProviderPayload is retained only for schema compatibility with legacy
	// orders. New checkout flows must use PaymentFact and PayloadHash instead.
	ProviderPayload string `json:"provider_payload" gorm:"type:text"`
}

func (o *SubscriptionOrder) Insert() error {
	if o.CreateTime == 0 {
		o.CreateTime = common.GetTimestamp()
	}
	return DB.Create(o).Error
}

func (o *SubscriptionOrder) Update() error {
	return DB.Save(o).Error
}

func GetSubscriptionOrderByTradeNo(tradeNo string) *SubscriptionOrder {
	order, err := FindSubscriptionOrderByTradeNo(tradeNo)
	if err != nil {
		return nil
	}
	return order
}

func FindSubscriptionOrderByTradeNo(tradeNo string) (*SubscriptionOrder, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return nil, ErrSubscriptionOrderNotFound
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionOrderNotFound
		}
		return nil, err
	}
	return &order, nil
}

// User subscription instance
type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_user_sub_active,priority:3"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_user_sub_active,priority:2"` // active/expired/cancelled

	Source string `json:"source" gorm:"type:varchar(32);default:'order'"` // order/admin

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	// Benefit policy is snapshotted when the entitlement is granted. Editing or
	// deleting the catalog plan must not change an already-paid subscription.
	BenefitSnapshotVersion  int    `json:"benefit_snapshot_version" gorm:"type:int;not null;default:0"`
	PlanTitle               string `json:"plan_title" gorm:"type:varchar(128)"`
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16)"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;not null;default:0"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`

	// Downgrade target group on expiry (snapshot from plan; empty = revert to PrevUserGroup)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Whether wallet fallback is allowed after this subscription's quota is exhausted (snapshot from plan)
	AllowWalletOverflow bool `json:"allow_wallet_overflow"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *UserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type SubscriptionSummary struct {
	Subscription *UserSubscription `json:"subscription"`
}

func calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error) {
	if plan == nil {
		return 0, errors.New("plan is nil")
	}
	if plan.DurationValue <= 0 && plan.DurationUnit != SubscriptionDurationCustom {
		return 0, errors.New("duration_value must be > 0")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		return start.AddDate(plan.DurationValue, 0, 0).Unix(), nil
	case SubscriptionDurationMonth:
		return start.AddDate(0, plan.DurationValue, 0).Unix(), nil
	case SubscriptionDurationDay:
		return start.Add(time.Duration(plan.DurationValue) * 24 * time.Hour).Unix(), nil
	case SubscriptionDurationHour:
		return start.Add(time.Duration(plan.DurationValue) * time.Hour).Unix(), nil
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 {
			return 0, errors.New("custom_seconds must be > 0")
		}
		if start.Unix() > math.MaxInt64-plan.CustomSeconds {
			return 0, errors.New("custom subscription duration overflows timestamp")
		}
		return start.Unix() + plan.CustomSeconds, nil
	default:
		return 0, fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func calcNextResetTimeForPolicy(base time.Time, resetPeriod string, customSeconds int64, endUnix int64) int64 {
	period := NormalizeResetPeriod(resetPeriod)
	if period == SubscriptionResetNever {
		return 0
	}
	var next time.Time
	switch period {
	case SubscriptionResetDaily:
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, 1)
	case SubscriptionResetWeekly:
		// Align to next Monday 00:00
		weekday := int(base.Weekday()) // Sunday=0
		// Convert to Monday=1..Sunday=7
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, daysUntil)
	case SubscriptionResetMonthly:
		// Align to first day of next month 00:00
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).
			AddDate(0, 1, 0)
	case SubscriptionResetCustom:
		if customSeconds <= 0 {
			return 0
		}
		if base.Unix() > math.MaxInt64-customSeconds {
			return 0
		}
		next = time.Unix(base.Unix()+customSeconds, 0)
	default:
		return 0
	}
	if endUnix > 0 && next.Unix() > endUnix {
		return 0
	}
	return next.Unix()
}

func calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64 {
	if plan == nil {
		return 0
	}
	return calcNextResetTimeForPolicy(base, plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds, endUnix)
}

func CountUserSubscriptionsByPlan(userId int, planId int) (int64, error) {
	if userId <= 0 || planId <= 0 {
		return 0, errors.New("invalid userId or planId")
	}
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", userId, planId).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func getUserGroupByIdTx(tx *gorm.DB, userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid userId")
	}
	if tx == nil {
		tx = DB
	}
	var group string
	if err := tx.Model(&User{}).Where("id = ?", userId).Select(commonGroupCol).Find(&group).Error; err != nil {
		return "", err
	}
	return group, nil
}

func downgradeUserGroupForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	downgradeGroup := strings.TrimSpace(sub.DowngradeGroup)
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	// Nothing to do if neither an explicit downgrade target nor an upgrade snapshot exists.
	if downgradeGroup == "" && upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	// If another active upgraded subscription exists, keep the current group.
	var activeSub UserSubscription
	activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> ''",
		sub.UserId, "active", now, sub.Id).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
		return "", nil
	}
	// Determine the downgrade target: an explicit downgrade group takes precedence,
	// otherwise revert to the group held before purchase (legacy behavior).
	target := downgradeGroup
	if target == "" {
		// Legacy behavior: only revert when the subscription actually elevated the user.
		if currentGroup != upgradeGroup {
			return "", nil
		}
		target = strings.TrimSpace(sub.PrevUserGroup)
	}
	if target == "" || target == currentGroup {
		return "", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
		Update("group", target).Error; err != nil {
		return "", err
	}
	return target, nil
}

func CreateUserSubscriptionFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, source string) (*UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if plan.MaxPurchasePerUser > 0 {
		var count int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND plan_id = ?", userId, plan.Id).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, ErrSubscriptionPurchaseLimitReached
		}
	}
	nowUnix := getDBTimestampTx(tx)
	now := time.Unix(nowUnix, 0)
	endUnix, err := calcPlanEndTime(now, plan)
	if err != nil {
		return nil, err
	}
	resetBase := now
	nextReset := calcNextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := getUserGroupByIdTx(tx, userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", upgradeGroup).Error; err != nil {
				return nil, err
			}
		}
	}
	allowWalletOverflow := true
	if plan.AllowWalletOverflow != nil {
		allowWalletOverflow = *plan.AllowWalletOverflow
	}
	sub := &UserSubscription{
		UserId:                  userId,
		PlanId:                  plan.Id,
		AmountTotal:             plan.TotalAmount,
		AmountUsed:              0,
		StartTime:               now.Unix(),
		EndTime:                 endUnix,
		Status:                  "active",
		Source:                  source,
		LastResetTime:           lastReset,
		NextResetTime:           nextReset,
		BenefitSnapshotVersion:  SubscriptionOrderSnapshotVersion,
		PlanTitle:               plan.Title,
		QuotaResetPeriod:        NormalizeResetPeriod(plan.QuotaResetPeriod),
		QuotaResetCustomSeconds: plan.QuotaResetCustomSeconds,
		UpgradeGroup:            upgradeGroup,
		PrevUserGroup:           prevGroup,
		DowngradeGroup:          strings.TrimSpace(plan.DowngradeGroup),
		AllowWalletOverflow:     allowWalletOverflow,
		CreatedAt:               common.GetTimestamp(),
		UpdatedAt:               common.GetTimestamp(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

func upsertSubscriptionTopUpTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}
	now := common.GetTimestamp()
	var topup TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = TopUp{
				UserId:        order.UserId,
				Amount:        0,
				Money:         order.Money,
				TradeNo:       order.TradeNo,
				PaymentMethod: order.PaymentMethod,
				CreateTime:    order.CreateTime,
				CompleteTime:  now,
				Status:        common.TopUpStatusSuccess,
			}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = common.TopUpStatusSuccess
	return tx.Save(&topup).Error
}

func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionOrderNotFound
			}
			return err
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return nil
		}
		order.Status = common.TopUpStatusExpired
		order.CompleteTime = common.GetTimestamp()
		return tx.Save(&order).Error
	})
}

// Admin bind (no payment). Creates a UserSubscription from a plan.
func AdminBindSubscription(userId int, planId int, sourceNote string) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid userId or planId")
	}
	var plan *SubscriptionPlan
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		plan, err = getSubscriptionPlanByIdForUpdateTx(tx, planId)
		if err != nil {
			return err
		}
		_, err = CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(plan.UpgradeGroup) != "" {
		_ = UpdateUserGroupCache(userId, plan.UpgradeGroup)
		return fmt.Sprintf("用户分组将升级到 %s", plan.UpgradeGroup), nil
	}
	return "", nil
}

func calcSubscriptionBalanceQuota(priceAmount string) (int, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(priceAmount))
	if err != nil || amount.IsNegative() {
		return 0, errors.New("订阅支付金额无效")
	}
	if amount.IsZero() {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("额度单位配置错误")
	}
	quota := amount.
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil().
		IntPart()
	return int(quota), nil
}

// PurchaseSubscriptionWithBalance creates a subscription by deducting the user's wallet quota.
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	if userId <= 0 || planId <= 0 {
		return errors.New("invalid userId or planId")
	}

	var logPlanTitle string
	var logMoney float64
	var chargedQuota int
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdForUpdateTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("套餐未启用")
		}
		if plan.PriceAmount < 0 {
			return errors.New("套餐价格不能为负数")
		}
		if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
			return errors.New("该套餐不允许使用余额兑换")
		}

		tradeNo := fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
		order, err := newSubscriptionOrderFromPlanTx(tx, userId, plan, tradeNo, PaymentMethodBalance, PaymentProviderBalance, SubscriptionCheckoutPolicy{
			Currency:         plan.Currency,
			CurrencySource:   SubscriptionCurrencySourceMerchantContract,
			AmountMultiplier: "1",
			CheckoutMode:     SubscriptionCheckoutModeOneTime,
		})
		if err != nil {
			return err
		}
		requiredQuota, err := calcSubscriptionBalanceQuota(order.ExpectedAmount)
		if err != nil {
			return err
		}

		var user User
		if err := lockForUpdate(tx).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if requiredQuota > 0 && user.Quota < requiredQuota {
			return errors.New("余额不足")
		}
		if requiredQuota > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", requiredQuota)).Error; err != nil {
				return err
			}
		}

		if err := tx.Create(order).Error; err != nil {
			return err
		}
		if _, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, PaymentMethodBalance); err != nil {
			return err
		}

		now := getDBTimestampTx(tx)
		fact := VerifiedSubscriptionPaymentFact{
			TradeNo:               tradeNo,
			Provider:              PaymentProviderBalance,
			ProviderEventId:       tradeNo,
			ProviderTransactionId: tradeNo,
			Amount:                order.ExpectedAmount,
			PaidAmount:            order.ExpectedAmount,
			Currency:              order.ExpectedCurrency,
			CurrencySource:        order.CurrencySource,
			ProductId:             order.ExpectedProductId,
			PaymentMethod:         PaymentMethodBalance,
			CheckoutMode:          order.ExpectedMode,
			SnapshotHash:          order.SnapshotHash,
			PayloadHash:           fmt.Sprintf("%x", common.Sha256Raw([]byte(fmt.Sprintf("charged_quota=%d", requiredQuota)))),
			PaidAt:                now,
		}
		if err := recordSubscriptionPaymentEvidenceTx(tx, order, fact, nil); err != nil {
			return err
		}
		if _, created, err := ensureSubscriptionReceiptTx(tx, order, fact, SubscriptionReceiptDispositionFulfilled, ""); err != nil {
			return err
		} else if !created {
			return ErrSubscriptionPaymentReceiptConflict
		}
		canonicalFact, err := canonicalSubscriptionPaymentFact(fact)
		if err != nil {
			return err
		}
		if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": now,
			"payment_fact":  canonicalFact,
		}).Error; err != nil {
			return err
		}

		logPlanTitle = plan.Title
		logMoney = order.Money
		chargedQuota = requiredQuota
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		return nil
	})
	if err != nil {
		return err
	}

	if chargedQuota > 0 {
		if err := cacheDecrUserQuota(userId, int64(chargedQuota)); err != nil {
			common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
		}
	}
	if upgradeGroup != "" {
		_ = UpdateUserGroupCache(userId, upgradeGroup)
	}
	msg := fmt.Sprintf("使用余额购买订阅成功，套餐: %s，支付金额: %.2f，扣除额度: %d", logPlanTitle, logMoney, chargedQuota)
	RecordLog(userId, LogTypeTopup, msg)
	return nil
}

// GetAllActiveUserSubscriptions returns all active subscriptions for a user.
func GetAllActiveUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	err := DB.Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

// HasActiveUserSubscription returns whether the user has any active subscription.
// This is a lightweight existence check to avoid heavy pre-consume transactions.
func HasActiveUserSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UserActiveSubscriptionsAllowWalletOverflow returns whether wallet balance may be used
// after the user's subscription quota is exhausted. A single active subscription that
// disallows wallet overflow (allow_wallet_overflow = false) blocks the fallback.
func UserActiveSubscriptionsAllowWalletOverflow(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var strictCount int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND allow_wallet_overflow = ?",
			userId, "active", now, false).
		Count(&strictCount).Error; err != nil {
		return false, err
	}
	return strictCount == 0, nil
}

// GetAllUserSubscriptions returns all subscriptions (active and expired) for a user.
func GetAllUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	var subs []UserSubscription
	err := DB.Where("user_id = ?", userId).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

func buildSubscriptionSummaries(subs []UserSubscription) []SubscriptionSummary {
	if len(subs) == 0 {
		return []SubscriptionSummary{}
	}
	result := make([]SubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		result = append(result, SubscriptionSummary{
			Subscription: &subCopy,
		})
	}
	return result
}

// AdminInvalidateUserSubscription marks a user subscription as cancelled and ends it immediately.
func AdminInvalidateUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":     "cancelled",
			"end_time":   now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if err := tx.Where("id = ?", userSubscriptionId).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
	ResetEpoch         int64
	OccurredAt         int64
}

// ExpireDueSubscriptions marks expired subscriptions and handles group downgrade.
func ExpireDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("status = ? AND end_time > 0 AND end_time <= ?", "active", now).
		Order("end_time asc, id asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	expiredCount := 0
	userIds := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if sub.UserId > 0 {
			userIds[sub.UserId] = struct{}{}
		}
	}
	for userId := range userIds {
		cacheGroup := ""
		err := DB.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userId, "active", now).
				Updates(map[string]interface{}{
					"status":     "expired",
					"updated_at": common.GetTimestamp(),
				})
			if res.Error != nil {
				return res.Error
			}
			expiredCount += int(res.RowsAffected)

			// If there's an active upgraded subscription, keep current group.
			var activeSub UserSubscription
			activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND upgrade_group <> ''",
				userId, "active", now).
				Order("end_time desc, id desc").
				Limit(1).
				Find(&activeSub)
			if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
				return nil
			}

			// Find the most recently expired subscription that defines a group transition
			// (an explicit downgrade target or an upgrade snapshot to revert).
			var lastExpired UserSubscription
			expiredQuery := tx.Where("user_id = ? AND status = ? AND (downgrade_group <> '' OR upgrade_group <> '')",
				userId, "expired").
				Order("end_time desc, id desc").
				Limit(1).
				Find(&lastExpired)
			if expiredQuery.Error != nil || expiredQuery.RowsAffected == 0 {
				return nil
			}
			currentGroup, err := getUserGroupByIdTx(tx, userId)
			if err != nil {
				return err
			}
			// An explicit downgrade group takes precedence; otherwise revert to the
			// group held before purchase (legacy behavior, only when the subscription
			// actually elevated the user).
			target := strings.TrimSpace(lastExpired.DowngradeGroup)
			if target == "" {
				upgradeGroup := strings.TrimSpace(lastExpired.UpgradeGroup)
				prevGroup := strings.TrimSpace(lastExpired.PrevUserGroup)
				if upgradeGroup == "" || prevGroup == "" {
					return nil
				}
				if currentGroup != upgradeGroup {
					return nil
				}
				target = prevGroup
			}
			if target == "" || target == currentGroup {
				return nil
			}
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", target).Error; err != nil {
				return err
			}
			cacheGroup = target
			return nil
		})
		if err != nil {
			return expiredCount, err
		}
		if cacheGroup != "" {
			_ = UpdateUserGroupCache(userId, cacheGroup)
		}
	}
	return expiredCount, nil
}

func maybeResetUserSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (bool, error) {
	if tx == nil || sub == nil {
		return false, errors.New("invalid reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return false, nil
	}
	if NormalizeResetPeriod(sub.QuotaResetPeriod) == SubscriptionResetNever {
		if sub.NextResetTime == 0 {
			return false, nil
		}
		sub.NextResetTime = 0
		return true, tx.Model(sub).Update("next_reset_time", 0).Error
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTimeForPolicy(base, sub.QuotaResetPeriod, sub.QuotaResetCustomSeconds, sub.EndTime)
	advanced := false
	if NormalizeResetPeriod(sub.QuotaResetPeriod) == SubscriptionResetCustom && next > 0 && next <= now {
		effectiveNow := now
		if sub.EndTime > 0 && effectiveNow > sub.EndTime {
			effectiveNow = sub.EndTime
		}
		steps := (effectiveNow - baseUnix) / sub.QuotaResetCustomSeconds
		if steps > 0 {
			advanced = true
			baseUnix += steps * sub.QuotaResetCustomSeconds
			base = time.Unix(baseUnix, 0)
			if baseUnix <= math.MaxInt64-sub.QuotaResetCustomSeconds {
				next = baseUnix + sub.QuotaResetCustomSeconds
				if sub.EndTime > 0 && next > sub.EndTime {
					next = 0
				}
			} else {
				next = 0
			}
		}
	}
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = calcNextResetTimeForPolicy(base, sub.QuotaResetPeriod, sub.QuotaResetCustomSeconds, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return true, tx.Save(sub).Error
		}
		return false, nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return true, tx.Save(sub).Error
}

// PreConsumeUserSubscription pre-consumes from any active subscription total quota.
func PreConsumeUserSubscription(requestId string, userId int, modelName string, quotaType int, amount int64) (*SubscriptionPreConsumeResult, error) {
	var result *SubscriptionPreConsumeResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = preConsumeUserSubscriptionTx(tx, requestId, userId, modelName, quotaType, amount, 0, 0, now)
		return err
	})
	return result, err
}

// PreConsumeUserSubscriptionWithToken atomically reserves subscription and
// token quota. The token adjustment has its own durable idempotency key inside
// the same transaction as the subscription pre-consume record.
func PreConsumeUserSubscriptionWithToken(requestId string, userId int, modelName string, quotaType int, amount int64, tokenId int, tokenAmount int) (*SubscriptionPreConsumeResult, error) {
	return preConsumeUserSubscriptionWithTokenAndSettlement(requestId, userId, modelName, quotaType, amount, tokenId, tokenAmount, nil, "")
}

// PreConsumeUserSubscriptionWithTokenAndSettlement commits the selected
// subscription, token reservation, and billing lifecycle root together before
// upstream work starts.
func PreConsumeUserSubscriptionWithTokenAndSettlement(requestId string, userId int, modelName string, quotaType int, amount int64, tokenId int, tokenAmount int, settlement BillingSettlementSpec) (*SubscriptionPreConsumeResult, error) {
	return preConsumeUserSubscriptionWithTokenAndSettlement(requestId, userId, modelName, quotaType, amount, tokenId, tokenAmount, &settlement, "")
}

func PreConsumeTaskSubmissionWithTokenAndSettlement(kind string, requestId string, userId int, modelName string, quotaType int, amount int64, tokenId int, tokenAmount int, settlement BillingSettlementSpec) (*SubscriptionPreConsumeResult, error) {
	return preConsumeUserSubscriptionWithTokenAndSettlement(requestId, userId, modelName, quotaType, amount, tokenId, tokenAmount, &settlement, kind)
}

func validateSubscriptionPreConsumeReplayTx(tx *gorm.DB, record *SubscriptionPreConsumeRecord, userId int, amount int64, tokenId int, tokenAmount int) error {
	if record.Status == "refunded" {
		return errors.New("subscription pre-consume already refunded")
	}
	if record.UserId != userId || record.PreConsumed != amount {
		return errors.New("subscription pre-consume retry does not match persisted request")
	}
	if record.TokenId == tokenId && record.TokenAmount == tokenAmount {
		return nil
	}
	if record.TokenId != 0 || record.TokenAmount != 0 || tokenId <= 0 || tokenAmount <= 0 {
		return errors.New("subscription pre-consume retry does not match persisted token reservation")
	}
	var legacy BillingAdjustmentIntent
	if err := tx.Where("request_id = ? AND operation = ?", record.RequestId, "subscription_preconsume_token").First(&legacy).Error; err != nil {
		return errors.New("legacy subscription token reservation cannot be verified")
	}
	if legacy.TokenId != tokenId || legacy.TokenQuotaDelta != -tokenAmount || legacy.Status != BillingAdjustmentStatusApplied {
		return errors.New("subscription pre-consume retry does not match legacy token reservation")
	}
	if err := tx.Model(&SubscriptionPreConsumeRecord{}).Where("id = ? AND token_id = 0 AND token_amount = 0", record.Id).
		Updates(map[string]interface{}{"token_id": tokenId, "token_amount": tokenAmount}).Error; err != nil {
		return err
	}
	record.TokenId = tokenId
	record.TokenAmount = tokenAmount
	return nil
}

func preConsumeUserSubscriptionWithTokenAndSettlement(requestId string, userId int, modelName string, quotaType int, amount int64, tokenId int, tokenAmount int, settlement *BillingSettlementSpec, submissionKind string) (*SubscriptionPreConsumeResult, error) {
	var result *SubscriptionPreConsumeResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if submissionKind != "" {
			if _, err := lockPreparingTaskSubmissionTx(tx, requestId, submissionKind); err != nil {
				return err
			}
		}
		var err error
		result, err = preConsumeUserSubscriptionTx(tx, requestId, userId, modelName, quotaType, amount, tokenId, tokenAmount, now)
		if err != nil {
			return err
		}
		if settlement != nil {
			root := *settlement
			root.SubscriptionId = result.UserSubscriptionId
			root.SubscriptionResetEpoch = result.ResetEpoch
			root.SubscriptionOccurredAt = result.OccurredAt
			if root.RequestId != requestId || root.UserId != userId || root.FundingSource != "subscription" || root.ReservedQuota != int(amount) || root.TokenId != tokenId {
				return errors.New("subscription billing settlement reservation mismatch")
			}
			if _, err := ensureBillingSettlementReservedTx(tx, root); err != nil {
				return err
			}
		}
		if tokenAmount == 0 {
			return nil
		}
		intent, err := ensureBillingAdjustmentPendingTx(tx, BillingAdjustmentSpec{
			RequestId:       requestId,
			Operation:       "subscription_preconsume_token",
			TokenId:         tokenId,
			TokenQuotaDelta: -tokenAmount,
		})
		if err != nil {
			return err
		}
		return applyBillingAdjustmentTx(tx, intent)
	})
	if err != nil {
		return nil, err
	}
	if tokenAmount != 0 {
		refreshBillingAdjustmentCaches(BillingAdjustmentIntent{TokenId: tokenId, TokenQuotaDelta: -tokenAmount})
	}
	return result, nil
}

func preConsumeUserSubscriptionTx(tx *gorm.DB, requestId string, userId int, _ string, _ int, amount int64, tokenId int, tokenAmount int, now int64) (*SubscriptionPreConsumeResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	requestId = strings.TrimSpace(requestId)
	if requestId == "" || len(requestId) > 128 {
		return nil, errors.New("requestId is invalid")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	returnValue := &SubscriptionPreConsumeResult{}
	err := func() error {
		var existing SubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if err := validateSubscriptionPreConsumeReplayTx(tx, &existing, userId, amount, tokenId, tokenAmount); err != nil {
				return err
			}
			var sub UserSubscription
			if err := tx.Where("id = ?", existing.UserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = existing.PreConsumed
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = sub.AmountUsed
			returnValue.AmountUsedAfter = sub.AmountUsed
			returnValue.ResetEpoch = existing.ResetEpoch
			returnValue.OccurredAt = existing.CreatedAt
			return nil
		}

		var subs []UserSubscription
		if err := lockForUpdate(tx).
			Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return errors.New("no active subscription")
		}
		if len(subs) == 0 {
			return errors.New("no active subscription")
		}
		for _, candidate := range subs {
			sub := candidate
			if _, err := maybeResetUserSubscriptionTx(tx, &sub, now); err != nil {
				return err
			}
			usedBefore := sub.AmountUsed
			if sub.AmountTotal > 0 {
				remain := sub.AmountTotal - usedBefore
				if remain < amount {
					continue
				}
			}
			claimId := common.GetUUID()
			record := &SubscriptionPreConsumeRecord{
				RequestId:          requestId,
				UserId:             userId,
				UserSubscriptionId: sub.Id,
				PreConsumed:        amount,
				ResetEpoch:         sub.LastResetTime,
				TokenId:            tokenId,
				TokenAmount:        tokenAmount,
				ClaimId:            claimId,
				Status:             "consumed",
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true,
			}).Create(record).Error; err != nil {
				return err
			}
			var persisted SubscriptionPreConsumeRecord
			if err := tx.Where("request_id = ?", requestId).First(&persisted).Error; err != nil {
				return err
			}
			if err := validateSubscriptionPreConsumeReplayTx(tx, &persisted, userId, amount, tokenId, tokenAmount); err != nil {
				return err
			}
			if persisted.ClaimId != claimId {
				var persistedSub UserSubscription
				if err := tx.Where("id = ?", persisted.UserSubscriptionId).First(&persistedSub).Error; err != nil {
					return err
				}
				returnValue.UserSubscriptionId = persistedSub.Id
				returnValue.PreConsumed = persisted.PreConsumed
				returnValue.AmountTotal = persistedSub.AmountTotal
				returnValue.AmountUsedBefore = persistedSub.AmountUsed
				returnValue.AmountUsedAfter = persistedSub.AmountUsed
				returnValue.ResetEpoch = persisted.ResetEpoch
				returnValue.OccurredAt = persisted.CreatedAt
				return nil
			}
			sub.AmountUsed += amount
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = amount
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = usedBefore
			returnValue.AmountUsedAfter = sub.AmountUsed
			returnValue.ResetEpoch = sub.LastResetTime
			returnValue.OccurredAt = persisted.CreatedAt
			return nil
		}
		return fmt.Errorf("subscription quota insufficient, need=%d", amount)
	}()
	if err != nil {
		return nil, err
	}
	return returnValue, nil
}

// RefundSubscriptionPreConsume is idempotent and refunds pre-consumed subscription quota by requestId.
func RefundSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return refundSubscriptionPreConsumeTx(tx, requestId)
	})
}

// ResetDueSubscriptions resets subscriptions whose next_reset_time has passed.
func ResetDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", now, "active").
		Order("next_reset_time asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, sub := range subs {
		subCopy := sub
		err := DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscription
			if err := lockForUpdate(tx).
				Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ?", subCopy.Id, now).
				First(&locked).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			changed, err := maybeResetUserSubscriptionTx(tx, &locked, now)
			if err != nil {
				return err
			}
			if !changed {
				return nil
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
	}
	return resetCount, nil
}

// Update subscription used amount by delta (positive consume more, negative refund).
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return postConsumeUserSubscriptionDeltaTx(tx, userSubscriptionId, delta)
	})
}

// postConsumeUserSubscriptionDeltaTx 在**调用方给定的事务内**调整订阅已用额度。
// 供已持有外层事务的调用方（如 RefundSubscriptionPreConsume）复用：绝不能在外层事务里
// 再开一个基于全局 DB 的嵌套事务——那会造成 ① 单连接池死锁；② 额度回写与幂等标记分处
// 两个事务，部分失败/重试下重复退额。公共入口 PostConsumeUserSubscriptionDelta 自带事务包裹。
func postConsumeUserSubscriptionDeltaTx(tx *gorm.DB, userSubscriptionId int, delta int64) error {
	_, err := postConsumeUserSubscriptionDeltaForEpochTx(tx, userSubscriptionId, delta, 0, 0)
	return err
}

// postConsumeUserSubscriptionDeltaForEpochTx returns skipped=true when a
// delayed old-period settlement reaches a subscription that has already reset.
// In that case the current period is left untouched.
func postConsumeUserSubscriptionDeltaForEpochTx(tx *gorm.DB, userSubscriptionId int, delta int64, resetEpoch int64, legacyOccurredAt int64) (bool, error) {
	if userSubscriptionId <= 0 {
		return false, errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return false, nil
	}
	var sub UserSubscription
	if err := lockForUpdate(tx).
		Where("id = ?", userSubscriptionId).
		First(&sub).Error; err != nil {
		return false, err
	}
	if resetEpoch > 0 {
		if sub.LastResetTime > resetEpoch {
			return true, nil
		}
		if sub.LastResetTime < resetEpoch {
			return false, errors.New("subscription reset epoch is ahead of persisted subscription")
		}
	} else if legacyOccurredAt > 0 && sub.LastResetTime > legacyOccurredAt {
		return true, nil
	}
	newUsed := sub.AmountUsed + delta
	if newUsed < 0 {
		return false, fmt.Errorf("subscription used would become negative, used=%d delta=%d", sub.AmountUsed, delta)
	}
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return false, fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	sub.AmountUsed = newUsed
	return false, tx.Save(&sub).Error
}
