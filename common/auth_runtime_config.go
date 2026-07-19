package common

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// AuthRuntimeConfig is the immutable runtime snapshot for login, registration,
// OAuth and registration-validation switches that may be changed together.
type AuthRuntimeConfig struct {
	PasswordLoginEnabled          bool
	PasswordRegisterEnabled       bool
	EmailVerificationEnabled      bool
	GitHubOAuthEnabled            bool
	LinuxDOOAuthEnabled           bool
	WeChatAuthEnabled             bool
	TelegramOAuthEnabled          bool
	TurnstileCheckEnabled         bool
	RegisterEnabled               bool
	EmailDomainRestrictionEnabled bool
	EmailAliasRestrictionEnabled  bool
	emailDomainWhitelist          []string

	GitHubClientID           string
	GitHubClientSecret       string
	LinuxDOClientID          string
	LinuxDOClientSecret      string
	LinuxDOMinimumTrustLevel int
	WeChatServerAddress      string
	WeChatServerToken        string
	WeChatAccountQRCodeURL   string
	TelegramBotToken         string
	TelegramBotName          string
	TurnstileSiteKey         string
	TurnstileSecretKey       string
}

var authRuntimeConfig atomic.Pointer[AuthRuntimeConfig]

func defaultAuthRuntimeConfig() AuthRuntimeConfig {
	return AuthRuntimeConfig{
		PasswordLoginEnabled:    true,
		PasswordRegisterEnabled: true,
		RegisterEnabled:         true,
		emailDomainWhitelist: []string{
			"gmail.com", "163.com", "126.com", "qq.com", "outlook.com", "hotmail.com",
			"icloud.com", "yahoo.com", "foxmail.com",
		},
	}
}

// GetAuthRuntimeConfig returns one immutable value snapshot.
func GetAuthRuntimeConfig() AuthRuntimeConfig {
	config := authRuntimeConfig.Load()
	if config == nil {
		return defaultAuthRuntimeConfig()
	}
	return *config
}

// EmailDomainAllowed checks this snapshot's immutable whitelist.
func (config AuthRuntimeConfig) EmailDomainAllowed(domain string) bool {
	for _, allowed := range config.emailDomainWhitelist {
		if domain == allowed {
			return true
		}
	}
	return false
}

// HasEmailDomainWhitelist reports whether this snapshot has at least one
// configured domain.
func (config AuthRuntimeConfig) HasEmailDomainWhitelist() bool {
	return len(config.emailDomainWhitelist) > 0
}

// EmailDomainWhitelistString serializes this snapshot's whitelist for the
// option API.
func (config AuthRuntimeConfig) EmailDomainWhitelistString() string {
	return strings.Join(config.emailDomainWhitelist, ",")
}

// IsAuthRuntimeOption reports whether key belongs to the coupled auth runtime
// snapshot.
func IsAuthRuntimeOption(key string) bool {
	switch key {
	case "PasswordLoginEnabled", "PasswordRegisterEnabled", "EmailVerificationEnabled",
		"GitHubOAuthEnabled", "LinuxDOOAuthEnabled", "WeChatAuthEnabled", "TelegramOAuthEnabled",
		"TurnstileCheckEnabled", "RegisterEnabled", "EmailDomainRestrictionEnabled",
		"EmailAliasRestrictionEnabled", "EmailDomainWhitelist", "GitHubClientId", "GitHubClientSecret",
		"LinuxDOClientId", "LinuxDOClientSecret", "LinuxDOMinimumTrustLevel", "WeChatServerAddress",
		"WeChatServerToken", "WeChatAccountQRCodeImageURL", "TelegramBotToken", "TelegramBotName",
		"TurnstileSiteKey", "TurnstileSecretKey":
		return true
	default:
		return false
	}
}

// ApplyAuthRuntimeOptions validates every recognized value, applies them to a
// copy of the current configuration and publishes the complete snapshot once.
func ApplyAuthRuntimeOptions(values map[string]string) (bool, error) {
	parsed := make(map[string]bool)
	stringValues := make(map[string]string)
	var linuxDOMinimumTrustLevel *int
	var emailDomainWhitelist []string
	emailDomainWhitelistSet := false
	for key, value := range values {
		if !IsAuthRuntimeOption(key) {
			continue
		}
		if key == "EmailDomainWhitelist" {
			emailDomainWhitelist = strings.Split(value, ",")
			emailDomainWhitelistSet = true
			continue
		}
		switch key {
		case "GitHubClientId", "GitHubClientSecret", "LinuxDOClientId", "LinuxDOClientSecret",
			"WeChatServerAddress", "WeChatServerToken", "WeChatAccountQRCodeImageURL",
			"TelegramBotToken", "TelegramBotName", "TurnstileSiteKey", "TurnstileSecretKey":
			stringValues[key] = value
			continue
		case "LinuxDOMinimumTrustLevel":
			intValue, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("invalid LinuxDOMinimumTrustLevel: %w", err)
			}
			linuxDOMinimumTrustLevel = &intValue
			continue
		}
		boolValue, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("invalid %s: %w", key, err)
		}
		parsed[key] = boolValue
	}
	if len(parsed) == 0 && len(stringValues) == 0 && linuxDOMinimumTrustLevel == nil && !emailDomainWhitelistSet {
		return false, nil
	}

	for {
		current := authRuntimeConfig.Load()
		next := defaultAuthRuntimeConfig()
		if current != nil {
			next = *current
		}
		for key, value := range parsed {
			switch key {
			case "PasswordLoginEnabled":
				next.PasswordLoginEnabled = value
			case "PasswordRegisterEnabled":
				next.PasswordRegisterEnabled = value
			case "EmailVerificationEnabled":
				next.EmailVerificationEnabled = value
			case "GitHubOAuthEnabled":
				next.GitHubOAuthEnabled = value
			case "LinuxDOOAuthEnabled":
				next.LinuxDOOAuthEnabled = value
			case "WeChatAuthEnabled":
				next.WeChatAuthEnabled = value
			case "TelegramOAuthEnabled":
				next.TelegramOAuthEnabled = value
			case "TurnstileCheckEnabled":
				next.TurnstileCheckEnabled = value
			case "RegisterEnabled":
				next.RegisterEnabled = value
			case "EmailDomainRestrictionEnabled":
				next.EmailDomainRestrictionEnabled = value
			case "EmailAliasRestrictionEnabled":
				next.EmailAliasRestrictionEnabled = value
			}
		}
		for key, value := range stringValues {
			switch key {
			case "GitHubClientId":
				next.GitHubClientID = value
			case "GitHubClientSecret":
				next.GitHubClientSecret = value
			case "LinuxDOClientId":
				next.LinuxDOClientID = value
			case "LinuxDOClientSecret":
				next.LinuxDOClientSecret = value
			case "WeChatServerAddress":
				next.WeChatServerAddress = value
			case "WeChatServerToken":
				next.WeChatServerToken = value
			case "WeChatAccountQRCodeImageURL":
				next.WeChatAccountQRCodeURL = value
			case "TelegramBotToken":
				next.TelegramBotToken = value
			case "TelegramBotName":
				next.TelegramBotName = value
			case "TurnstileSiteKey":
				next.TurnstileSiteKey = value
			case "TurnstileSecretKey":
				next.TurnstileSecretKey = value
			}
		}
		if linuxDOMinimumTrustLevel != nil {
			next.LinuxDOMinimumTrustLevel = *linuxDOMinimumTrustLevel
		}
		if emailDomainWhitelistSet {
			next.emailDomainWhitelist = emailDomainWhitelist
		}

		snapshot := next
		if authRuntimeConfig.CompareAndSwap(current, &snapshot) {
			return true, nil
		}
	}
}
