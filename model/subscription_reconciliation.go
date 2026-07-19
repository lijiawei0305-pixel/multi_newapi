package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	SubscriptionReviewActionGrant              = "grant"
	SubscriptionReviewActionClose              = "close"
	SubscriptionReviewActionExternallyRefunded = "externally_refunded"
)

type SubscriptionOrderReviewItem struct {
	Order          SubscriptionOrder                   `json:"order"`
	Receipts       []SubscriptionPaymentReceipt        `json:"receipts"`
	RelatedReceipt *SubscriptionPaymentReceipt         `json:"related_receipt,omitempty"`
	Evidence       []SubscriptionPaymentEvidence       `json:"evidence"`
	Decisions      []SubscriptionPaymentReviewDecision `json:"decisions"`
}

func ListSubscriptionOrderReviews(offset int, limit int) ([]SubscriptionOrderReviewItem, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	query := DB.Model(&SubscriptionOrder{}).Where("review_status = ?", SubscriptionOrderReviewRequired)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []SubscriptionOrder
	if err := query.Order("review_time desc, id desc").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	if len(orders) == 0 {
		return []SubscriptionOrderReviewItem{}, total, nil
	}
	orderIds := make([]int, 0, len(orders))
	relatedReceiptIds := make([]int, 0, len(orders))
	for _, order := range orders {
		orderIds = append(orderIds, order.Id)
		if order.ReviewRelatedReceiptId > 0 {
			relatedReceiptIds = append(relatedReceiptIds, order.ReviewRelatedReceiptId)
		}
	}
	var receipts []SubscriptionPaymentReceipt
	if err := DB.Where("order_id IN ?", orderIds).Order("id asc").Find(&receipts).Error; err != nil {
		return nil, 0, err
	}
	var relatedReceipts []SubscriptionPaymentReceipt
	if len(relatedReceiptIds) > 0 {
		if err := DB.Where("id IN ?", relatedReceiptIds).Find(&relatedReceipts).Error; err != nil {
			return nil, 0, err
		}
	}
	var evidence []SubscriptionPaymentEvidence
	if err := DB.Where("order_id IN ?", orderIds).Order("id asc").Find(&evidence).Error; err != nil {
		return nil, 0, err
	}
	var decisions []SubscriptionPaymentReviewDecision
	if err := DB.Where("order_id IN ?", orderIds).Order("id asc").Find(&decisions).Error; err != nil {
		return nil, 0, err
	}
	receiptsByOrder := make(map[int][]SubscriptionPaymentReceipt, len(orders))
	for _, receipt := range receipts {
		receiptsByOrder[receipt.OrderId] = append(receiptsByOrder[receipt.OrderId], receipt)
	}
	relatedById := make(map[int]SubscriptionPaymentReceipt, len(relatedReceipts))
	for _, receipt := range relatedReceipts {
		relatedById[receipt.Id] = receipt
	}
	evidenceByOrder := make(map[int][]SubscriptionPaymentEvidence, len(orders))
	for _, event := range evidence {
		evidenceByOrder[event.OrderId] = append(evidenceByOrder[event.OrderId], event)
	}
	decisionsByOrder := make(map[int][]SubscriptionPaymentReviewDecision, len(orders))
	for _, decision := range decisions {
		decisionsByOrder[decision.OrderId] = append(decisionsByOrder[decision.OrderId], decision)
	}
	items := make([]SubscriptionOrderReviewItem, 0, len(orders))
	for _, order := range orders {
		item := SubscriptionOrderReviewItem{
			Order:     order,
			Receipts:  receiptsByOrder[order.Id],
			Evidence:  evidenceByOrder[order.Id],
			Decisions: decisionsByOrder[order.Id],
		}
		if related, ok := relatedById[order.ReviewRelatedReceiptId]; ok {
			relatedCopy := related
			item.RelatedReceipt = &relatedCopy
		}
		items = append(items, item)
	}
	return items, total, nil
}

func subscriptionPaymentFactFromReceipt(order *SubscriptionOrder, receipt *SubscriptionPaymentReceipt) VerifiedSubscriptionPaymentFact {
	return VerifiedSubscriptionPaymentFact{
		TradeNo:               order.TradeNo,
		Provider:              receipt.Provider,
		ProviderEventId:       receipt.ProviderEventId,
		ProviderTransactionId: receipt.ProviderTransactionId,
		ProviderCheckoutId:    receipt.ProviderCheckoutId,
		Amount:                receipt.Amount,
		PaidAmount:            receipt.PaidAmount,
		Currency:              receipt.Currency,
		CurrencySource:        receipt.CurrencySource,
		ProductId:             receipt.ProductId,
		PaymentMethod:         receipt.PaymentMethod,
		CheckoutMode:          receipt.CheckoutMode,
		SnapshotHash:          receipt.SnapshotHash,
		PayloadHash:           receipt.PayloadHash,
		PaidAt:                receipt.PaidAt,
	}
}

func ResolveSubscriptionOrderReview(orderId int, actorId int, receiptId int, action string, note string) error {
	action = strings.ToLower(strings.TrimSpace(action))
	note = strings.TrimSpace(note)
	if orderId <= 0 || actorId <= 0 || receiptId <= 0 || note == "" || len(note) > 4000 {
		return ErrSubscriptionReviewActionInvalid
	}
	switch action {
	case SubscriptionReviewActionGrant, SubscriptionReviewActionClose, SubscriptionReviewActionExternallyRefunded:
	default:
		return ErrSubscriptionReviewActionInvalid
	}
	var upgradeGroup string
	var affectedUserId int
	resolved := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where("id = ?", orderId).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionOrderNotFound
			}
			return err
		}
		if order.ReviewStatus == SubscriptionOrderReviewResolved {
			if order.ResolutionAction == action && order.ResolutionReceiptId == receiptId &&
				order.ResolutionNote == note && order.ResolvedBy == actorId {
				return nil
			}
			return ErrSubscriptionReviewAlreadyResolved
		}
		if order.ReviewStatus != SubscriptionOrderReviewRequired {
			return ErrSubscriptionOrderStatusInvalid
		}
		var receipt SubscriptionPaymentReceipt
		if err := tx.Where("id = ?", receiptId).First(&receipt).Error; err != nil {
			return err
		}
		receiptBelongsToOrder := receipt.OrderId == order.Id
		if !receiptBelongsToOrder && receipt.Id != order.ReviewRelatedReceiptId {
			return ErrSubscriptionPaymentReceiptConflict
		}
		if action == SubscriptionReviewActionGrant && !receiptBelongsToOrder {
			return ErrSubscriptionPaymentReceiptConflict
		}

		now := getDBTimestampTx(tx)
		updates := map[string]interface{}{
			"review_status":         SubscriptionOrderReviewResolved,
			"resolution_action":     action,
			"resolution_note":       note,
			"resolution_receipt_id": receipt.Id,
			"resolved_by":           actorId,
			"resolved_at":           now,
		}
		if action == SubscriptionReviewActionGrant {
			if order.Status == common.TopUpStatusSuccess {
				return ErrSubscriptionReviewActionInvalid
			}
			snapshot, err := decodeSubscriptionCheckoutSnapshot(&order)
			if err != nil {
				return err
			}
			plan, err := snapshot.planForFulfillment()
			if err != nil {
				return err
			}
			// An administrator explicitly approving a paid review is the override
			// boundary for a purchase-limit mismatch captured after checkout.
			plan.MaxPurchasePerUser = 0
			var user User
			if err := lockForUpdate(tx).Select("id").Where("id = ?", order.UserId).First(&user).Error; err != nil {
				return err
			}
			if _, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "review"); err != nil {
				return err
			}
			if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
				return err
			}
			factText, err := canonicalSubscriptionPaymentFact(subscriptionPaymentFactFromReceipt(&order, &receipt))
			if err != nil {
				return err
			}
			if err := tx.Model(&SubscriptionPaymentReceipt{}).Where("id = ?", receipt.Id).Updates(map[string]interface{}{
				"disposition": SubscriptionReceiptDispositionFulfilled,
				"reason":      boundedSubscriptionReviewReason(errors.New(receipt.Reason + "; manually approved during subscription payment review")),
			}).Error; err != nil {
				return err
			}
			updates["status"] = common.TopUpStatusSuccess
			updates["complete_time"] = now
			updates["payment_fact"] = factText
			upgradeGroup = strings.TrimSpace(snapshot.Plan.UpgradeGroup)
			affectedUserId = order.UserId
		} else if receiptBelongsToOrder {
			disposition := SubscriptionReceiptDispositionReviewClosed
			if action == SubscriptionReviewActionExternallyRefunded {
				disposition = SubscriptionReceiptDispositionExternallyRefunded
			}
			if err := tx.Model(&SubscriptionPaymentReceipt{}).Where("id = ?", receipt.Id).
				Update("disposition", disposition).Error; err != nil {
				return err
			}
		}
		if action != SubscriptionReviewActionGrant && order.Status == SubscriptionOrderStatusReconciliationRequired {
			// A resolved, non-granted order must be terminal rather than looking
			// indefinitely pending to downstream order views.
			updates["status"] = common.TopUpStatusFailed
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND review_status = ?", order.Id, SubscriptionOrderReviewRequired).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSubscriptionReviewAlreadyResolved
		}
		decision := &SubscriptionPaymentReviewDecision{
			OrderId: order.Id, DecisionKey: common.GetUUID(), ReceiptId: receipt.Id,
			Action: action, Note: note, ActorId: actorId, ReviewTime: order.ReviewTime, CreatedAt: now,
		}
		if err := tx.Create(decision).Error; err != nil {
			return err
		}
		resolved = true
		return nil
	})
	if err != nil {
		return err
	}
	if !resolved {
		return nil
	}
	RecordLog(actorId, LogTypeManage, fmt.Sprintf("处理订阅支付核账，订单ID: %d，回执ID: %d，动作: %s", orderId, receiptId, action))
	if affectedUserId > 0 {
		if upgradeGroup != "" {
			_ = UpdateUserGroupCache(affectedUserId, upgradeGroup)
		}
		RecordLog(affectedUserId, LogTypeTopup, fmt.Sprintf("订阅支付核账后补发权益，订单ID: %d", orderId))
	}
	return nil
}
