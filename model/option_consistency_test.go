package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setOptionMapValueForTest(t *testing.T, key, value string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if existed {
			common.OptionMap[key] = original
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func optionMapValueForTest(key string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[key]
}

func TestUpdateOptionRejectsInvalidValueBeforePersistenceOrPublication(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open("file:option-validation?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	originalGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalGroups)) })
	valid := `{"default":"Default","vip":"VIP"}`
	setOptionMapValueForTest(t, "UserUsableGroups", valid)
	require.NoError(t, UpdateOption("UserUsableGroups", valid))

	err = UpdateOption("UserUsableGroups", `{"broken":`)
	require.Error(t, err)
	assert.Equal(t, valid, optionMapValueForTest("UserUsableGroups"))
	assert.Equal(t, map[string]string{"default": "Default", "vip": "VIP"}, setting.GetUserUsableGroupsCopy())

	var stored Option
	require.NoError(t, testDB.First(&stored, "key = ?", "UserUsableGroups").Error)
	assert.Equal(t, valid, stored.Value)
}

func TestUpdateOptionDatabaseFailureDoesNotPublishRuntimeValue(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open("file:option-db-failure?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	setOptionMapValueForTest(t, "Notice", "before")
	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = UpdateOption("Notice", "after")
	require.Error(t, err)
	assert.Equal(t, "before", optionMapValueForTest("Notice"))
}

func TestUpdateOptionRejectsInvalidRegisteredConfigBeforePersistence(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open("file:option-config-validation?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	setOptionMapValueForTest(t, "claude.default_max_tokens", `{"claude-3":4096}`)
	err = UpdateOption("claude.default_max_tokens", `{"broken":`)
	require.Error(t, err)
	assert.Equal(t, `{"claude-3":4096}`, optionMapValueForTest("claude.default_max_tokens"))

	var count int64
	require.NoError(t, testDB.Model(&Option{}).Where("key = ?", "claude.default_max_tokens").Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateOptionsBulkPublishesOneNativePaymentSnapshot(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open("file:option-native-payment-bulk?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	originalConfig := setting.GetNativePaymentConfig()
	t.Cleanup(func() { setting.SetNativePaymentConfig(originalConfig) })
	setting.SetNativePaymentConfig(setting.NativePaymentConfig{
		WechatPayAppID: "old-wx-app",
		AlipayAppID:    "old-alipay-app",
	})

	values := map[string]string{
		"WechatPayEnabled":     "true",
		"WechatPayAppID":       "new-wx-app",
		"WechatPayMchID":       "new-wx-merchant",
		"WechatPayAPIv3Key":    "new-wx-api-key",
		"WechatPayCertSerial":  "new-wx-serial",
		"WechatPayPrivateKey":  "new-wx-private-key",
		"WechatPayPublicKeyID": "new-wx-public-key-id",
		"WechatPayPublicKey":   "new-wx-public-key",
		"AlipayEnabled":        "true",
		"AlipayAppID":          "new-alipay-app",
		"AlipayPrivateKey":     "new-alipay-private-key",
		"AlipayPublicKey":      "new-alipay-public-key",
		"AlipaySellerID":       "new-alipay-seller",
		"AlipayReturnURL":      "https://example.test/return",
		"AlipaySandbox":        "true",
	}
	for key := range values {
		setOptionMapValueForTest(t, key, "old")
	}

	require.NoError(t, UpdateOptionsBulk(values))
	assert.Equal(t, setting.NativePaymentConfig{
		WechatPayEnabled:     true,
		WechatPayAppID:       "new-wx-app",
		WechatPayMchID:       "new-wx-merchant",
		WechatPayAPIv3Key:    "new-wx-api-key",
		WechatPayCertSerial:  "new-wx-serial",
		WechatPayPrivateKey:  "new-wx-private-key",
		WechatPayPublicKeyID: "new-wx-public-key-id",
		WechatPayPublicKey:   "new-wx-public-key",
		AlipayEnabled:        true,
		AlipayAppID:          "new-alipay-app",
		AlipayPrivateKey:     "new-alipay-private-key",
		AlipayPublicKey:      "new-alipay-public-key",
		AlipaySellerID:       "new-alipay-seller",
		AlipayReturnURL:      "https://example.test/return",
		AlipaySandbox:        true,
	}, setting.GetNativePaymentConfig())
	for key, want := range values {
		assert.Equal(t, want, optionMapValueForTest(key), key)
	}

	var stored []Option
	require.NoError(t, testDB.Order("key").Find(&stored).Error)
	assert.Len(t, stored, len(values))
}

func TestUpdateOptionsBulkPublishesOneRateLimitSnapshot(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open("file:option-rate-limit-bulk?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	originalConfig := setting.GetModelRequestRateLimitConfig()
	t.Cleanup(func() {
		_, err := setting.ApplyModelRequestRateLimitOptions(map[string]string{
			"ModelRequestRateLimitEnabled":         strconv.FormatBool(originalConfig.Enabled),
			"ModelRequestRateLimitDurationMinutes": strconv.Itoa(originalConfig.DurationMinutes),
			"ModelRequestRateLimitCount":           strconv.Itoa(originalConfig.Count),
			"ModelRequestRateLimitSuccessCount":    strconv.Itoa(originalConfig.SuccessCount),
			"ModelRequestRateLimitGroup":           originalConfig.GroupRateLimitsJSONString(),
		})
		require.NoError(t, err)
	})

	values := map[string]string{
		"ModelRequestRateLimitEnabled":         "true",
		"ModelRequestRateLimitDurationMinutes": "15",
		"ModelRequestRateLimitCount":           "200",
		"ModelRequestRateLimitSuccessCount":    "180",
		"ModelRequestRateLimitGroup":           `{"vip":[400,360]}`,
	}
	for key := range values {
		setOptionMapValueForTest(t, key, "old")
	}

	require.NoError(t, UpdateOptionsBulk(values))
	config := setting.GetModelRequestRateLimitConfig()
	assert.True(t, config.Enabled)
	assert.Equal(t, 15, config.DurationMinutes)
	assert.Equal(t, 200, config.Count)
	assert.Equal(t, 180, config.SuccessCount)
	total, success, found := config.GroupLimit("vip")
	assert.True(t, found)
	assert.Equal(t, 400, total)
	assert.Equal(t, 360, success)
	for key, want := range values {
		assert.Equal(t, want, optionMapValueForTest(key), key)
	}
}
