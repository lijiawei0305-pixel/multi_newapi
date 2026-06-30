package realpay

import (
	"testing"

	"github.com/smartwalle/alipay/v3"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func aliNoti(appID, outNo, txn, status, total, seller string) *alipay.Notification {
	return &alipay.Notification{
		AppId:       appID,
		OutTradeNo:  outNo,
		TradeNo:     txn,
		TradeStatus: alipay.TradeStatus(status),
		TotalAmount: total,
		SellerId:    seller,
	}
}

// TestAliNotificationToInfo_Success TRADE_SUCCESS → 正确映射（订单号/交易号/金额元/渠道）。
func TestAliNotificationToInfo_Success(t *testing.T) {
	info, err := aliNotificationToInfo(aliNoti("2021app", "RCG-1", "2013xxx", "TRADE_SUCCESS", "29.80", "2088seller"), "2021app", "2088seller")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Success || info.OrderNo != "RCG-1" || info.TxnID != "2013xxx" {
		t.Fatalf("unexpected info %+v", info)
	}
	if info.PaidAmount != 29.80 {
		t.Fatalf("paid=%v want 29.80", info.PaidAmount)
	}
	if info.Provider != payment.ProviderAlipay {
		t.Fatalf("provider=%v", info.Provider)
	}
}

// TestAliNotificationToInfo_Finished TRADE_FINISHED 同样视为已收款。
func TestAliNotificationToInfo_Finished(t *testing.T) {
	info, err := aliNotificationToInfo(aliNoti("2021app", "RCG-2", "x", "TRADE_FINISHED", "1.00", ""), "2021app", "")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Success {
		t.Fatal("TRADE_FINISHED should be success")
	}
}

// TestAliNotificationToInfo_WaitBuyerPay 未付状态 → Success=false。
func TestAliNotificationToInfo_WaitBuyerPay(t *testing.T) {
	info, err := aliNotificationToInfo(aliNoti("2021app", "RCG-3", "x", "WAIT_BUYER_PAY", "1.00", ""), "2021app", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Success {
		t.Fatal("WAIT_BUYER_PAY must not be success")
	}
}

// TestAliNotificationToInfo_AppMismatch app_id 不符 → 拒绝。
func TestAliNotificationToInfo_AppMismatch(t *testing.T) {
	if _, err := aliNotificationToInfo(aliNoti("evil", "RCG-4", "x", "TRADE_SUCCESS", "1.00", ""), "2021app", ""); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("app_id mismatch want %s, got %v", payment.CodeCallbackInvalid, err)
	}
}

// TestAliNotificationToInfo_SellerMismatch 配置 sellerID 且通知 seller_id 不符 → 拒绝。
func TestAliNotificationToInfo_SellerMismatch(t *testing.T) {
	if _, err := aliNotificationToInfo(aliNoti("2021app", "RCG-5", "x", "TRADE_SUCCESS", "1.00", "2088other"), "2021app", "2088mine"); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("seller mismatch want %s, got %v", payment.CodeCallbackInvalid, err)
	}
}

// TestAliNotificationToInfo_MissingOrderNo 缺 out_trade_no → ErrCallbackInvalid。
func TestAliNotificationToInfo_MissingOrderNo(t *testing.T) {
	if _, err := aliNotificationToInfo(aliNoti("2021app", "", "x", "TRADE_SUCCESS", "1.00", ""), "2021app", ""); apperr.CodeOf(err) != payment.CodeCallbackInvalid {
		t.Fatalf("missing order_no want %s, got %v", payment.CodeCallbackInvalid, err)
	}
}
