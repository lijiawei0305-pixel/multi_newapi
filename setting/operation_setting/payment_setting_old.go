/**
此文件为旧版支付设置文件，如需增加新的参数、变量等，请在 payment_setting.go 中添加
This file is the old version of the payment settings file. If you need to add new parameters, variables, etc., please add them in payment_setting.go
*/

package operation_setting

import (
	"github.com/QuantumNous/new-api/common"
)

var PayAddress = ""
var CustomCallbackAddress = ""
var EpayId = ""
var EpayKey = ""
var Price = 7.3
var MinTopUp = 1
var USDExchangeRate = 7.3

var PayMethods = []map[string]string{
	{
		"name": "支付宝",
		"icon": "SiAlipay",
		"type": "alipay",
	},
	{
		"name": "微信",
		"icon": "SiWechat",
		"type": "wxpay",
	},
	{
		"name":      "自定义1",
		"icon":      "LuCreditCard",
		"type":      "custom1",
		"min_topup": "50",
	},
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	PayMethods = make([]map[string]string, 0)
	return common.Unmarshal([]byte(jsonString), &PayMethods)
}

func PayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(PayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	for _, payMethod := range PayMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}

// OfficialPayMethodTypes are the PayMethods `type` keys reserved for the official
// in-process WeChat/Alipay SDK flow (handled by internal/mtwire, settled via the
// real notify/query). They must never be sent to the Epay gateway: Epay would
// receive an unknown channel and fail. Kept distinct from Epay's own
// "wxpay"/"alipay" so the two never collide.
var OfficialPayMethodTypes = map[string]bool{
	"wxpay_official":  true,
	"alipay_official": true,
}

// IsOfficialPayMethod reports whether a PayMethods type routes to the official
// in-process SDK rather than Epay. Epay entry points reject these (defense in
// depth): the buyer UI already routes official channels to the dedicated flow.
func IsOfficialPayMethod(method string) bool {
	return OfficialPayMethodTypes[method]
}
