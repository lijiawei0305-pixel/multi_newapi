package payment

import (
	"time"
)

// stage 日志阶段名（PAY-OBS-01）。不得包含二维码全文、私钥、签名或完整回调 body。
const (
	StageRequestReceived      = "request_received"
	StageLocalOrderCreated    = "local_order_created"
	StageProviderAttemptStart = "provider_attempt_start"
	StageProviderAttemptEnd   = "provider_attempt_end"
	StagePayURLPersisted      = "pay_url_persisted"
	StageResponseSent         = "response_sent"
	StageNotifyReceived       = "notify_received"
	StageSignatureVerified    = "signature_verified"
	StageOrderClaimed         = "order_claimed"
	StageLedgerCommitted      = "ledger_transaction_committed"
	StageCacheInvalidated     = "cache_invalidated"
	StageOrderCredited        = "order_credited"
	StageAckSent              = "ack_sent"
	StageQueryStart           = "provider_query_start"
	StageQueryEnd             = "provider_query_end"
)

// logStage 输出精简阶段日志（经 g.logf；默认 no-op）。
func (g *Gateway) logStage(stage, orderNo string, provider Provider, durationMs int64, extra string) {
	if extra != "" {
		g.logf("payment: stage=%s order_no=%s provider=%s duration_ms=%d %s",
			stage, orderNo, provider, durationMs, extra)
		return
	}
	g.logf("payment: stage=%s order_no=%s provider=%s duration_ms=%d",
		stage, orderNo, provider, durationMs)
}

// elapsedMs 自 t0 起的毫秒数。
func elapsedMs(t0 time.Time) int64 {
	return time.Since(t0).Milliseconds()
}
