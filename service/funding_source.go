package service

import "github.com/QuantumNous/new-api/model"

// ---------------------------------------------------------------------------
// FundingSource — 资金来源接口（钱包 or 订阅）
// ---------------------------------------------------------------------------

// FundingSource 抽象了预扣费的资金来源。
type FundingSource interface {
	// Source 返回资金来源标识："wallet" 或 "subscription"
	Source() string
}

// ---------------------------------------------------------------------------
// WalletFunding — 钱包资金来源实现
// ---------------------------------------------------------------------------

type WalletFunding struct {
	userId   int
	consumed int // 实际预扣的用户额度
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

// ---------------------------------------------------------------------------
// SubscriptionFunding — 订阅资金来源实现
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId      string
	userId         int
	modelName      string
	amount         int64 // 预扣的订阅额度（subConsume）
	subscriptionId int
	preConsumed    int64
	resetEpoch     int64
	occurredAt     int64
	// 以下字段在 PreConsume 成功后填充，供 RelayInfo 同步使用
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsumeWithToken(tokenId int, tokenAmount int) error {
	res, err := model.PreConsumeUserSubscriptionWithToken(s.requestId, s.userId, s.modelName, 0, s.amount, tokenId, tokenAmount)
	if err != nil {
		return err
	}
	return s.applyPreConsumeResult(res)
}

func (s *SubscriptionFunding) PreConsumeWithTokenAndSettlement(tokenId int, tokenAmount int, settlement model.BillingSettlementSpec) error {
	res, err := model.PreConsumeUserSubscriptionWithTokenAndSettlement(s.requestId, s.userId, s.modelName, 0, s.amount, tokenId, tokenAmount, settlement)
	if err != nil {
		return err
	}
	return s.applyPreConsumeResult(res)
}

func (s *SubscriptionFunding) PreConsumeTaskSubmissionWithTokenAndSettlement(kind string, tokenId int, tokenAmount int, settlement model.BillingSettlementSpec) error {
	res, err := model.PreConsumeTaskSubmissionWithTokenAndSettlement(kind, s.requestId, s.userId, s.modelName, 0, s.amount, tokenId, tokenAmount, settlement)
	if err != nil {
		return err
	}
	return s.applyPreConsumeResult(res)
}

func (s *SubscriptionFunding) applyPreConsumeResult(res *model.SubscriptionPreConsumeResult) error {
	s.subscriptionId = res.UserSubscriptionId
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	s.resetEpoch = res.ResetEpoch
	s.occurredAt = res.OccurredAt
	// 获取订阅计划信息
	if planInfo, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(res.UserSubscriptionId); err == nil && planInfo != nil {
		s.PlanId = planInfo.PlanId
		s.PlanTitle = planInfo.PlanTitle
	}
	return nil
}
