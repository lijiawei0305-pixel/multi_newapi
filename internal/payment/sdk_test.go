package payment

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestStubPaySDKSignRoundTrip(t *testing.T) {
	ctx := context.Background()
	sdk := NewStubPaySDK("topsecret")

	raw := sdk.Encode(CallbackInfo{OrderNo: "PAY9", Success: true, PaidAmount: 12.5, TxnID: "wx-1"})
	info, err := sdk.Verify(ctx, ProviderWxpay, raw)
	if err != nil {
		t.Fatalf("Verify valid sign: %v", err)
	}
	if info.OrderNo != "PAY9" || !info.Success || info.PaidAmount != 12.5 || info.Provider != ProviderWxpay {
		t.Fatalf("info = %+v", info)
	}
}

func TestStubPaySDKRejectsTamperedSign(t *testing.T) {
	ctx := context.Background()
	sdk := NewStubPaySDK("topsecret")
	raw := sdk.Encode(CallbackInfo{OrderNo: "PAY9", Success: true, PaidAmount: 10})

	// 篡改金额但保留旧签名 → 验签失败。
	tampered := []byte(`{"order_no":"PAY9","success":true,"amount":999,"txn_id":"","sign":"deadbeef"}`)
	if _, err := sdk.Verify(ctx, ProviderWxpay, tampered); apperr.CodeOf(err) != CodeSignInvalid {
		t.Fatalf("tampered sign code = %q, want %q", apperr.CodeOf(err), CodeSignInvalid)
	}

	// 另一密钥验签同一报文 → 失败。
	other := NewStubPaySDK("different")
	if _, err := other.Verify(ctx, ProviderWxpay, raw); apperr.CodeOf(err) != CodeSignInvalid {
		t.Fatalf("cross-secret code = %q, want %q", apperr.CodeOf(err), CodeSignInvalid)
	}
}

func TestStubPaySDKRejectsMalformed(t *testing.T) {
	ctx := context.Background()
	sdk := NewStubPaySDK("s")
	for _, raw := range [][]byte{
		[]byte("not json"),
		[]byte(`{"success":true}`), // 缺 order_no
	} {
		_, err := sdk.Verify(ctx, ProviderAlipay, raw)
		if got := apperr.CodeOf(err); got != CodeCallbackInvalid {
			t.Fatalf("malformed %q code = %q, want %q", raw, got, CodeCallbackInvalid)
		}
	}
}

func TestStubPaySDKCreatePay(t *testing.T) {
	cred, err := NewStubPaySDK("s").CreatePay(context.Background(), PayRequest{Provider: ProviderWxpay, OrderNo: "PAY1"})
	if err != nil || cred.PayURL == "" {
		t.Fatalf("CreatePay = %+v, err=%v", cred, err)
	}
}
