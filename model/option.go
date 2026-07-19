package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)

	// 添加原有的系统配置
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	authRuntime := common.GetAuthRuntimeConfig()
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(authRuntime.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(authRuntime.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(authRuntime.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(authRuntime.GitHubOAuthEnabled)
	common.OptionMap["LinuxDOOAuthEnabled"] = strconv.FormatBool(authRuntime.LinuxDOOAuthEnabled)
	common.OptionMap["TelegramOAuthEnabled"] = strconv.FormatBool(authRuntime.TelegramOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(authRuntime.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(authRuntime.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(authRuntime.RegisterEnabled)
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(common.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(common.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(common.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(common.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(common.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(common.DrawingEnabled)
	common.OptionMap["TaskEnabled"] = strconv.FormatBool(common.TaskEnabled)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(common.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(common.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(authRuntime.EmailDomainRestrictionEnabled)
	common.OptionMap["EmailAliasRestrictionEnabled"] = strconv.FormatBool(authRuntime.EmailAliasRestrictionEnabled)
	common.OptionMap["EmailDomainWhitelist"] = authRuntime.EmailDomainWhitelistString()
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPFrom"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(common.SMTPSSLEnabled)
	common.OptionMap["SMTPStartTLSEnabled"] = strconv.FormatBool(common.SMTPStartTLSEnabled)
	common.OptionMap["SMTPInsecureSkipVerify"] = strconv.FormatBool(common.SMTPInsecureSkipVerify)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(common.SMTPForceAuthLogin)
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["Footer"] = common.Footer
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["Logo"] = common.Logo
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["WorkerUrl"] = system_setting.WorkerUrl
	common.OptionMap["WorkerValidKey"] = system_setting.WorkerValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(system_setting.WorkerAllowHttpImageRequestEnabled)
	common.OptionMap["PayAddress"] = ""
	common.OptionMap["CustomCallbackAddress"] = ""
	common.OptionMap["EpayId"] = ""
	common.OptionMap["EpayKey"] = ""
	common.OptionMap["Price"] = strconv.FormatFloat(operation_setting.Price, 'f', -1, 64)
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(operation_setting.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["MinTopUp"] = strconv.Itoa(operation_setting.MinTopUp)
	common.OptionMap["StripeMinTopUp"] = strconv.Itoa(setting.StripeMinTopUp)
	common.OptionMap["StripeApiSecret"] = setting.StripeApiSecret
	common.OptionMap["StripeWebhookSecret"] = setting.StripeWebhookSecret
	common.OptionMap["StripePriceId"] = setting.StripePriceId
	common.OptionMap["StripeUnitPrice"] = strconv.FormatFloat(setting.StripeUnitPrice, 'f', -1, 64)
	common.OptionMap["StripePromotionCodesEnabled"] = strconv.FormatBool(setting.StripePromotionCodesEnabled)
	// 微信支付 / 支付宝原生支付凭据必须从同一个不可变快照读取，避免初始化期间混用字段。
	nativePayment := setting.GetNativePaymentConfig()
	common.OptionMap["WechatPayEnabled"] = strconv.FormatBool(nativePayment.WechatPayEnabled)
	common.OptionMap["WechatPayAppID"] = nativePayment.WechatPayAppID
	common.OptionMap["WechatPayMchID"] = nativePayment.WechatPayMchID
	common.OptionMap["WechatPayAPIv3Key"] = nativePayment.WechatPayAPIv3Key
	common.OptionMap["WechatPayCertSerial"] = nativePayment.WechatPayCertSerial
	common.OptionMap["WechatPayPrivateKey"] = nativePayment.WechatPayPrivateKey
	common.OptionMap["WechatPayPublicKeyID"] = nativePayment.WechatPayPublicKeyID
	common.OptionMap["WechatPayPublicKey"] = nativePayment.WechatPayPublicKey
	common.OptionMap["AlipayEnabled"] = strconv.FormatBool(nativePayment.AlipayEnabled)
	common.OptionMap["AlipayAppID"] = nativePayment.AlipayAppID
	common.OptionMap["AlipayPrivateKey"] = nativePayment.AlipayPrivateKey
	common.OptionMap["AlipayPublicKey"] = nativePayment.AlipayPublicKey
	common.OptionMap["AlipaySellerID"] = nativePayment.AlipaySellerID
	common.OptionMap["AlipayReturnURL"] = nativePayment.AlipayReturnURL
	common.OptionMap["AlipaySandbox"] = strconv.FormatBool(nativePayment.AlipaySandbox)
	common.OptionMap["CreemApiKey"] = setting.CreemApiKey
	common.OptionMap["CreemProducts"] = setting.CreemProducts
	common.OptionMap["CreemTestMode"] = strconv.FormatBool(setting.CreemTestMode)
	common.OptionMap["CreemWebhookSecret"] = setting.CreemWebhookSecret
	common.OptionMap["WaffoEnabled"] = strconv.FormatBool(setting.WaffoEnabled)
	common.OptionMap["WaffoApiKey"] = setting.WaffoApiKey
	common.OptionMap["WaffoPrivateKey"] = setting.WaffoPrivateKey
	common.OptionMap["WaffoPublicCert"] = setting.WaffoPublicCert
	common.OptionMap["WaffoSandboxPublicCert"] = setting.WaffoSandboxPublicCert
	common.OptionMap["WaffoSandboxApiKey"] = setting.WaffoSandboxApiKey
	common.OptionMap["WaffoSandboxPrivateKey"] = setting.WaffoSandboxPrivateKey
	common.OptionMap["WaffoSandbox"] = strconv.FormatBool(setting.WaffoSandbox)
	common.OptionMap["WaffoMerchantId"] = setting.WaffoMerchantId
	common.OptionMap["WaffoNotifyUrl"] = setting.WaffoNotifyUrl
	common.OptionMap["WaffoReturnUrl"] = setting.WaffoReturnUrl
	common.OptionMap["WaffoSubscriptionReturnUrl"] = setting.WaffoSubscriptionReturnUrl
	common.OptionMap["WaffoCurrency"] = setting.WaffoCurrency
	common.OptionMap["WaffoUnitPrice"] = strconv.FormatFloat(setting.WaffoUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoMinTopUp"] = strconv.Itoa(setting.WaffoMinTopUp)
	common.OptionMap["WaffoPayMethods"] = setting.WaffoPayMethods2JsonString()
	common.OptionMap["WaffoPancakeMerchantID"] = setting.WaffoPancakeMerchantID
	common.OptionMap["WaffoPancakePrivateKey"] = setting.WaffoPancakePrivateKey
	common.OptionMap["WaffoPancakeReturnURL"] = setting.WaffoPancakeReturnURL
	common.OptionMap["WaffoPancakeUnitPrice"] = strconv.FormatFloat(setting.WaffoPancakeUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoPancakeMinTopUp"] = strconv.Itoa(setting.WaffoPancakeMinTopUp)
	common.OptionMap["WaffoPancakeStoreID"] = setting.WaffoPancakeStoreID
	common.OptionMap["WaffoPancakeProductID"] = setting.WaffoPancakeProductID
	common.OptionMap["TopupGroupRatio"] = common.TopupGroupRatio2JSONString()
	common.OptionMap["Chats"] = setting.Chats2JsonString()
	common.OptionMap["AutoGroups"] = setting.AutoGroups2JsonString()
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(setting.DefaultUseAutoGroup)
	common.OptionMap["PayMethods"] = operation_setting.PayMethods2JsonString()
	common.OptionMap["GitHubClientId"] = ""
	common.OptionMap["GitHubClientSecret"] = ""
	common.OptionMap["TelegramBotToken"] = ""
	common.OptionMap["TelegramBotName"] = ""
	common.OptionMap["WeChatServerAddress"] = ""
	common.OptionMap["WeChatServerToken"] = ""
	common.OptionMap["WeChatAccountQRCodeImageURL"] = ""
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["QuotaForNewUser"] = strconv.Itoa(common.QuotaForNewUser)
	common.OptionMap["QuotaForInviter"] = strconv.Itoa(common.QuotaForInviter)
	common.OptionMap["QuotaForInvitee"] = strconv.Itoa(common.QuotaForInvitee)
	common.OptionMap["QuotaRemindThreshold"] = strconv.Itoa(common.QuotaRemindThreshold)
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(common.PreConsumedQuota)
	modelRateLimit := setting.GetModelRequestRateLimitConfig()
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(modelRateLimit.Count)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(modelRateLimit.DurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(modelRateLimit.SuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = modelRateLimit.GroupRateLimitsJSONString()
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["UserUsableGroups"] = setting.UserUsableGroups2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	common.OptionMap["TopUpLink"] = common.TopUpLink
	//common.OptionMap["ChatLink"] = common.ChatLink
	//common.OptionMap["ChatLink2"] = common.ChatLink2
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(common.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(common.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = common.DataExportDefaultTime
	common.OptionMap["DefaultCollapseSidebar"] = strconv.FormatBool(common.DefaultCollapseSidebar)
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(setting.MjNotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(setting.MjAccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(setting.MjModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(setting.MjForwardUrlEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(setting.MjActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(setting.CheckSensitiveEnabled)
	common.OptionMap["DemoSiteEnabled"] = strconv.FormatBool(operation_setting.DemoSiteEnabled)
	common.OptionMap["SelfUseModeEnabled"] = strconv.FormatBool(operation_setting.SelfUseModeEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(modelRateLimit.Enabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnPromptEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(setting.StopOnSensitiveEnabled)
	common.OptionMap["SensitiveWords"] = setting.SensitiveWordsToString()
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(setting.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = operation_setting.AutomaticDisableKeywordsToString()
	common.OptionMap["AutomaticDisableStatusCodes"] = operation_setting.AutomaticDisableStatusCodesToString()
	common.OptionMap["AutomaticRetryStatusCodes"] = operation_setting.AutomaticRetryStatusCodesToString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())

	// 自动添加所有注册的模型配置
	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	for k, v := range modelConfigs {
		common.OptionMap[k] = v
	}

	common.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, err := AllOption()
	if err != nil {
		common.SysLog("failed to load options from database: " + err.Error())
		return
	}
	coupledOptionChanges := make(map[string]string)
	for _, option := range options {
		common.OptionMapRWMutex.RLock()
		current, loaded := common.OptionMap[option.Key]
		common.OptionMapRWMutex.RUnlock()
		if loaded && current == option.Value {
			continue
		}
		if common.IsAuthRuntimeOption(option.Key) || setting.IsNativePaymentOption(option.Key) || setting.IsModelRequestRateLimitOption(option.Key) {
			coupledOptionChanges[option.Key] = option.Value
			continue
		}
		err := updateOptionMap(option.Key, option.Value)
		if err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
	if len(coupledOptionChanges) > 0 {
		if err := updateOptionMaps(coupledOptionChanges); err != nil {
			common.SysLog("failed to update coupled runtime options: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func UpdateOption(key string, value string) error {
	if err := ValidateOptionValue(key, value); err != nil {
		return err
	}
	if DB == nil {
		return fmt.Errorf("option database is unavailable")
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: key}
		if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
			return err
		}
		option.Value = value
		return tx.Save(&option).Error
	}); err != nil {
		return err
	}
	return updateOptionMap(key, value)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then publishes their runtime state in one pass. Coupled native
// payment credentials are exposed through one immutable snapshot. If any DB
// write fails the whole transaction rolls back and no in-memory state is
// touched.
func UpdateOptionsBulk(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	for key, value := range values {
		if err := ValidateOptionValue(key, value); err != nil {
			return err
		}
	}
	if DB == nil {
		return fmt.Errorf("option database is unavailable")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for k, v := range values {
			option := Option{Key: k}
			if err := tx.FirstOrCreate(&option, Option{Key: k}).Error; err != nil {
				return err
			}
			option.Value = v
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return updateOptionMaps(values)
}

// ValidateOptionValue validates values that have structured or scalar runtime
// representations without publishing them. Persistence always happens only
// after this succeeds, preventing malformed settings from surviving a restart.
func ValidateOptionValue(key string, value string) error {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 2 {
		handled, err := config.GlobalConfig.ValidateUpdate(parts[0], map[string]string{parts[1]: value})
		if handled {
			if err != nil {
				return fmt.Errorf("invalid value for option %s: %w", key, err)
			}
			return nil
		}
	}

	var err error
	switch key {
	case "Chats", "PayMethods":
		var decoded []map[string]string
		err = common.Unmarshal([]byte(value), &decoded)
	case "AutoGroups":
		var decoded []string
		err = common.Unmarshal([]byte(value), &decoded)
	case "WaffoPayMethods":
		var decoded []constant.WaffoPayMethod
		err = common.Unmarshal([]byte(value), &decoded)
	case "TopupGroupRatio", "ModelRatio", "ModelPrice", "CacheRatio", "CreateCacheRatio", "CompletionRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio":
		var decoded map[string]float64
		err = common.Unmarshal([]byte(value), &decoded)
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(value)
	case "GroupGroupRatio":
		var decoded map[string]map[string]float64
		err = common.Unmarshal([]byte(value), &decoded)
	case "UserUsableGroups":
		var decoded map[string]string
		err = common.Unmarshal([]byte(value), &decoded)
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(value)
	case "AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(value)
	}
	if err != nil {
		return fmt.Errorf("invalid value for option %s: %w", key, err)
	}

	if strings.HasSuffix(key, "Permission") {
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("invalid integer for option %s: %w", key, err)
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" || key == "AlipaySandbox" || key == "WaffoSandbox" || key == "CreemTestMode" {
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid boolean for option %s: %w", key, err)
		}
	}

	intOptions := map[string]struct{}{
		"SMTPPort": {}, "MinTopUp": {}, "StripeMinTopUp": {}, "WaffoMinTopUp": {},
		"WaffoPancakeMinTopUp": {}, "LinuxDOMinimumTrustLevel": {}, "QuotaForNewUser": {},
		"QuotaForInviter": {}, "QuotaForInvitee": {}, "QuotaRemindThreshold": {}, "PreConsumedQuota": {},
		"ModelRequestRateLimitCount": {}, "ModelRequestRateLimitDurationMinutes": {},
		"ModelRequestRateLimitSuccessCount": {}, "RetryTimes": {}, "DataExportInterval": {},
		"StreamCacheQueueLength": {},
	}
	if _, ok := intOptions[key]; ok {
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("invalid integer for option %s: %w", key, err)
		}
	}
	floatOptions := map[string]struct{}{
		"Price": {}, "USDExchangeRate": {}, "StripeUnitPrice": {}, "WaffoUnitPrice": {},
		"WaffoPancakeUnitPrice": {}, "ChannelDisableThreshold": {}, "QuotaPerUnit": {},
	}
	if _, ok := floatOptions[key]; ok {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid number for option %s: %w", key, err)
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("invalid non-finite number for option %s", key)
		}
	}
	return nil
}

func updateOptionMap(key string, value string) (err error) {
	return updateOptionMaps(map[string]string{key: value})
}

// updateOptionMaps publishes a set of committed runtime options. Native
// payment credentials are collected and stored as one immutable snapshot only
// after every value has passed validation, so readers never observe a partial
// bulk rotation.
func updateOptionMaps(values map[string]string) error {
	for key, value := range values {
		if err := ValidateOptionValue(key, value); err != nil {
			return err
		}
	}
	for key, value := range values {
		if err := updateOptionRuntime(key, value); err != nil {
			return err
		}
	}
	if _, err := setting.ApplyNativePaymentOptions(values); err != nil {
		return err
	}
	if _, err := common.ApplyAuthRuntimeOptions(values); err != nil {
		return err
	}
	_, err := setting.ApplyModelRequestRateLimitOptions(values)
	return err
}

func updateOptionRuntime(key string, value string) (err error) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	common.OptionMap[key] = value

	// 检查是否是模型配置 - 使用更规范的方式处理
	if handled, err := handleConfigUpdate(key, value); handled {
		return err
	}

	// 处理传统配置项...
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled", "PasswordLoginEnabled", "EmailVerificationEnabled",
			"GitHubOAuthEnabled", "LinuxDOOAuthEnabled", "WeChatAuthEnabled", "TelegramOAuthEnabled",
			"TurnstileCheckEnabled", "RegisterEnabled", "EmailDomainRestrictionEnabled",
			"EmailAliasRestrictionEnabled":
			// updateOptionMaps atomically publishes the complete auth runtime snapshot.
		case "AutomaticDisableChannelEnabled":
			common.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			common.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			common.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			// 兼容旧字段：同步到新配置 general_setting.quota_display_type（运行时生效）
			// true -> USD, false -> TOKENS
			newVal := "USD"
			if !boolValue {
				newVal = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": newVal})
			}
		case "DisplayTokenStatEnabled":
			common.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			common.DrawingEnabled = boolValue
		case "TaskEnabled":
			common.TaskEnabled = boolValue
		case "DataExportEnabled":
			common.DataExportEnabled = boolValue
		case "DefaultCollapseSidebar":
			common.DefaultCollapseSidebar = boolValue
		case "MjNotifyEnabled":
			setting.MjNotifyEnabled = boolValue
		case "MjAccountFilterEnabled":
			setting.MjAccountFilterEnabled = boolValue
		case "MjModeClearEnabled":
			setting.MjModeClearEnabled = boolValue
		case "MjForwardUrlEnabled":
			setting.MjForwardUrlEnabled = boolValue
		case "MjActionCheckSuccessEnabled":
			setting.MjActionCheckSuccessEnabled = boolValue
		case "CheckSensitiveEnabled":
			setting.CheckSensitiveEnabled = boolValue
		case "DemoSiteEnabled":
			operation_setting.DemoSiteEnabled = boolValue
		case "SelfUseModeEnabled":
			operation_setting.SelfUseModeEnabled = boolValue
		case "CheckSensitiveOnPromptEnabled":
			setting.CheckSensitiveOnPromptEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			// updateOptionMaps atomically publishes the complete rate-limit snapshot.
		case "StopOnSensitiveEnabled":
			setting.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			common.SMTPSSLEnabled = boolValue
		case "SMTPStartTLSEnabled":
			common.SMTPStartTLSEnabled = boolValue
		case "SMTPInsecureSkipVerify":
			common.SMTPInsecureSkipVerify = boolValue
		case "SMTPForceAuthLogin":
			common.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			system_setting.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			setting.DefaultUseAutoGroup = boolValue
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	switch key {
	case "EmailDomainWhitelist":
		// updateOptionMaps atomically publishes the whitelist with its auth switches.
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ServerAddress":
		system_setting.ServerAddress = value
	case "WorkerUrl":
		system_setting.WorkerUrl = value
	case "WorkerValidKey":
		system_setting.WorkerValidKey = value
	case "PayAddress":
		operation_setting.PayAddress = value
	case "Chats":
		err = setting.UpdateChatsByJsonString(value)
	case "AutoGroups":
		err = setting.UpdateAutoGroupsByJsonString(value)
	case "CustomCallbackAddress":
		operation_setting.CustomCallbackAddress = value
	case "EpayId":
		operation_setting.EpayId = value
	case "EpayKey":
		operation_setting.EpayKey = value
	case "Price":
		operation_setting.Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		operation_setting.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		operation_setting.MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		setting.StripeApiSecret = value
	case "StripeWebhookSecret":
		setting.StripeWebhookSecret = value
	case "StripePriceId":
		setting.StripePriceId = value
	case "StripeUnitPrice":
		setting.StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		setting.StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		setting.StripePromotionCodesEnabled = value == "true"
	// 微信支付 / 支付宝原生支付配置由 updateOptionMaps 在本函数返回后一次性发布。
	case "CreemApiKey":
		setting.CreemApiKey = value
	case "CreemProducts":
		setting.CreemProducts = value
	case "CreemTestMode":
		setting.CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		setting.CreemWebhookSecret = value
	case "WaffoEnabled":
		setting.WaffoEnabled = value == "true"
	case "WaffoApiKey":
		setting.WaffoApiKey = value
	case "WaffoPrivateKey":
		setting.WaffoPrivateKey = value
	case "WaffoPublicCert":
		setting.WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		setting.WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		setting.WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		setting.WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		setting.WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		setting.WaffoMerchantId = value
	case "WaffoNotifyUrl":
		setting.WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		setting.WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		setting.WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		setting.WaffoCurrency = value
	case "WaffoUnitPrice":
		setting.WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		setting.WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeMerchantID":
		setting.WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		setting.WaffoPancakePrivateKey = value
	case "WaffoPancakeReturnURL":
		setting.WaffoPancakeReturnURL = value
	case "WaffoPancakeStoreID":
		setting.WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		setting.WaffoPancakeProductID = value
	case "WaffoPancakeUnitPrice":
		setting.WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		setting.WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "TopupGroupRatio":
		err = common.UpdateTopupGroupRatioByJSONString(value)
	case "GitHubClientId", "GitHubClientSecret", "LinuxDOClientId", "LinuxDOClientSecret",
		"LinuxDOMinimumTrustLevel", "WeChatServerAddress", "WeChatServerToken",
		"WeChatAccountQRCodeImageURL", "TelegramBotToken", "TelegramBotName",
		"TurnstileSiteKey", "TurnstileSecretKey":
		// updateOptionMaps atomically publishes credentials with their auth switches.
	case "Footer":
		common.Footer = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "QuotaForNewUser":
		common.QuotaForNewUser, _ = strconv.Atoi(value)
	case "QuotaForInviter":
		common.QuotaForInviter, _ = strconv.Atoi(value)
	case "QuotaForInvitee":
		common.QuotaForInvitee, _ = strconv.Atoi(value)
	case "QuotaRemindThreshold":
		common.QuotaRemindThreshold, _ = strconv.Atoi(value)
	case "PreConsumedQuota":
		common.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount", "ModelRequestRateLimitDurationMinutes",
		"ModelRequestRateLimitSuccessCount", "ModelRequestRateLimitGroup":
		// updateOptionMaps atomically publishes the complete rate-limit snapshot.
	case "RetryTimes":
		common.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		common.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		common.DataExportDefaultTime = value
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "UserUsableGroups":
		err = setting.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "TopUpLink":
		common.TopUpLink = value
	//case "ChatLink":
	//	common.ChatLink = value
	//case "ChatLink2":
	//	common.ChatLink2 = value
	case "ChannelDisableThreshold":
		common.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		common.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		setting.SensitiveWordsFromString(value)
	case "AutomaticDisableKeywords":
		operation_setting.AutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = operation_setting.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = operation_setting.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		setting.StreamCacheQueueLength, _ = strconv.Atoi(value)
	case "PayMethods":
		err = operation_setting.UpdatePayMethodsByJsonString(value)
	case "WaffoPayMethods":
		// WaffoPayMethods is read directly from OptionMap via setting.GetWaffoPayMethods().
		// The value is already stored in OptionMap at the top of this function (line: common.OptionMap[key] = value).
		// No additional in-memory variable to update.
	}
	return err
}

// handleConfigUpdate 处理分层配置更新，返回是否已处理
func handleConfigUpdate(key, value string) (bool, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false, nil // 不是分层配置
	}

	configName := parts[0]
	configKey := parts[1]

	configMap := map[string]string{
		configKey: value,
	}
	handled, err := config.GlobalConfig.ApplyUpdate(configName, configMap)
	if !handled || err != nil {
		return handled, err
	}

	// 特定配置的后处理
	if configName == "performance_setting" {
		performance_setting.UpdateAndSync()
	} else if configName == "tool_price_setting" {
		operation_setting.RebuildToolPriceIndex()
	} else if configName == "billing_setting" {
		InvalidatePricingCache()
		ratio_setting.InvalidateExposedDataCache()
	} else if configName == "theme" {
		system_setting.UpdateAndSyncTheme()
	}

	return true, nil // 已处理
}
