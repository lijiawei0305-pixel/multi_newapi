package realpay

import (
	"testing"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// TestYuanToFen 微信下单金额换算：元 → 分（四舍五入）。
func TestYuanToFen(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{29.80, 2980}, {0.01, 1}, {1, 100}, {73.00, 7300}, {0.1, 10}, {99.99, 9999},
	}
	for _, c := range cases {
		if got := yuanToFen(c.in); got != c.want {
			t.Fatalf("yuanToFen(%.2f)=%d want %d", c.in, got, c.want)
		}
	}
}

func wxTx(appid, mchid, outNo, state, txn string, totalFen int64) *payments.Transaction {
	return &payments.Transaction{
		Appid:         core.String(appid),
		Mchid:         core.String(mchid),
		OutTradeNo:    core.String(outNo),
		TradeState:    core.String(state),
		TransactionId: core.String(txn),
		Amount:        &payments.TransactionAmount{Total: core.Int64(totalFen)},
	}
}

// TestWxTransactionToInfo_Success 验签解密后的成功交易 → 正确映射（状态/金额分→元/交易号/渠道）。
func TestWxTransactionToInfo_Success(t *testing.T) {
	info, err := wxTransactionToInfo(wxTx("wxapp", "1600000", "RCG-1", "SUCCESS", "4200xxx", 2980), "wxapp", "1600000")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Success || info.OrderNo != "RCG-1" || info.TxnID != "4200xxx" {
		t.Fatalf("unexpected info %+v", info)
	}
	if info.PaidAmount != 29.80 {
		t.Fatalf("paid=%v want 29.80 (分→元)", info.PaidAmount)
	}
	if info.Provider != payment.ProviderWxpay {
		t.Fatalf("provider=%v", info.Provider)
	}
}

// TestWxTransactionToInfo_NotSuccess 非 SUCCESS 状态 → Success=false（不入账）。
func TestWxTransactionToInfo_NotSuccess(t *testing.T) {
	info, err := wxTransactionToInfo(wxTx("wxapp", "1600000", "RCG-2", "CLOSED", "", 2980), "wxapp", "1600000")
	if err != nil {
		t.Fatal(err)
	}
	if info.Success {
		t.Fatal("CLOSED must not be success")
	}
}

// TestWxTransactionToInfo_MerchantMismatch appid/mchid 与本商户不符 → 拒绝(反伪造/串站)。
func TestWxTransactionToInfo_MerchantMismatch(t *testing.T) {
	if _, err := wxTransactionToInfo(wxTx("evil", "1600000", "RCG-3", "SUCCESS", "x", 2980), "wxapp", "1600000"); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("appid mismatch want %s, got %v", payment.CodeCallbackInvalid, err)
	}
	if _, err := wxTransactionToInfo(wxTx("wxapp", "9999", "RCG-3", "SUCCESS", "x", 2980), "wxapp", "1600000"); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("mchid mismatch want %s, got %v", payment.CodeCallbackInvalid, err)
	}
}

// TestWxTransactionToInfo_MissingFields 缺关键字段 → ErrCallbackInvalid。
func TestWxTransactionToInfo_MissingFields(t *testing.T) {
	if _, err := wxTransactionToInfo(&payments.Transaction{}, "a", "b"); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("missing fields want %s, got %v", payment.CodeCallbackInvalid, err)
	}
	if _, err := wxTransactionToInfo(nil, "a", "b"); err == nil {
		t.Fatal("nil tx must error")
	}
}
