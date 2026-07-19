package common

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyAuthRuntimeOptionsPublishesCompleteSnapshot(t *testing.T) {
	original := GetAuthRuntimeConfig()
	t.Cleanup(func() {
		_, err := ApplyAuthRuntimeOptions(map[string]string{
			"PasswordLoginEnabled":          strconv.FormatBool(original.PasswordLoginEnabled),
			"PasswordRegisterEnabled":       strconv.FormatBool(original.PasswordRegisterEnabled),
			"EmailVerificationEnabled":      strconv.FormatBool(original.EmailVerificationEnabled),
			"GitHubOAuthEnabled":            strconv.FormatBool(original.GitHubOAuthEnabled),
			"LinuxDOOAuthEnabled":           strconv.FormatBool(original.LinuxDOOAuthEnabled),
			"WeChatAuthEnabled":             strconv.FormatBool(original.WeChatAuthEnabled),
			"TelegramOAuthEnabled":          strconv.FormatBool(original.TelegramOAuthEnabled),
			"TurnstileCheckEnabled":         strconv.FormatBool(original.TurnstileCheckEnabled),
			"RegisterEnabled":               strconv.FormatBool(original.RegisterEnabled),
			"EmailDomainRestrictionEnabled": strconv.FormatBool(original.EmailDomainRestrictionEnabled),
			"EmailAliasRestrictionEnabled":  strconv.FormatBool(original.EmailAliasRestrictionEnabled),
			"EmailDomainWhitelist":          original.EmailDomainWhitelistString(),
			"GitHubClientId":                original.GitHubClientID,
			"GitHubClientSecret":            original.GitHubClientSecret,
			"LinuxDOClientId":               original.LinuxDOClientID,
			"LinuxDOClientSecret":           original.LinuxDOClientSecret,
			"LinuxDOMinimumTrustLevel":      strconv.Itoa(original.LinuxDOMinimumTrustLevel),
			"WeChatServerAddress":           original.WeChatServerAddress,
			"WeChatServerToken":             original.WeChatServerToken,
			"WeChatAccountQRCodeImageURL":   original.WeChatAccountQRCodeURL,
			"TelegramBotToken":              original.TelegramBotToken,
			"TelegramBotName":               original.TelegramBotName,
			"TurnstileSiteKey":              original.TurnstileSiteKey,
			"TurnstileSecretKey":            original.TurnstileSecretKey,
		})
		require.NoError(t, err)
	})

	handled, err := ApplyAuthRuntimeOptions(map[string]string{
		"PasswordLoginEnabled":          "false",
		"PasswordRegisterEnabled":       "false",
		"EmailVerificationEnabled":      "true",
		"GitHubOAuthEnabled":            "true",
		"LinuxDOOAuthEnabled":           "true",
		"WeChatAuthEnabled":             "true",
		"TelegramOAuthEnabled":          "true",
		"TurnstileCheckEnabled":         "true",
		"RegisterEnabled":               "false",
		"EmailDomainRestrictionEnabled": "true",
		"EmailAliasRestrictionEnabled":  "true",
		"EmailDomainWhitelist":          "example.test,example.org",
		"GitHubClientId":                "github-client",
		"GitHubClientSecret":            "github-secret",
		"LinuxDOClientId":               "linuxdo-client",
		"LinuxDOClientSecret":           "linuxdo-secret",
		"LinuxDOMinimumTrustLevel":      "3",
		"WeChatServerAddress":           "https://wechat.example.test",
		"WeChatServerToken":             "wechat-secret",
		"WeChatAccountQRCodeImageURL":   "https://wechat.example.test/qr.png",
		"TelegramBotToken":              "telegram-secret",
		"TelegramBotName":               "example_bot",
		"TurnstileSiteKey":              "turnstile-site",
		"TurnstileSecretKey":            "turnstile-secret",
	})
	require.NoError(t, err)
	assert.True(t, handled)

	config := GetAuthRuntimeConfig()
	assert.False(t, config.PasswordLoginEnabled)
	assert.False(t, config.PasswordRegisterEnabled)
	assert.True(t, config.EmailVerificationEnabled)
	assert.True(t, config.GitHubOAuthEnabled)
	assert.True(t, config.LinuxDOOAuthEnabled)
	assert.True(t, config.WeChatAuthEnabled)
	assert.True(t, config.TelegramOAuthEnabled)
	assert.True(t, config.TurnstileCheckEnabled)
	assert.False(t, config.RegisterEnabled)
	assert.True(t, config.EmailDomainRestrictionEnabled)
	assert.True(t, config.EmailAliasRestrictionEnabled)
	assert.Equal(t, "example.test,example.org", config.EmailDomainWhitelistString())
	assert.True(t, config.EmailDomainAllowed("example.org"))
	assert.Equal(t, "github-client", config.GitHubClientID)
	assert.Equal(t, "github-secret", config.GitHubClientSecret)
	assert.Equal(t, "linuxdo-client", config.LinuxDOClientID)
	assert.Equal(t, "linuxdo-secret", config.LinuxDOClientSecret)
	assert.Equal(t, 3, config.LinuxDOMinimumTrustLevel)
	assert.Equal(t, "https://wechat.example.test", config.WeChatServerAddress)
	assert.Equal(t, "wechat-secret", config.WeChatServerToken)
	assert.Equal(t, "https://wechat.example.test/qr.png", config.WeChatAccountQRCodeURL)
	assert.Equal(t, "telegram-secret", config.TelegramBotToken)
	assert.Equal(t, "example_bot", config.TelegramBotName)
	assert.Equal(t, "turnstile-site", config.TurnstileSiteKey)
	assert.Equal(t, "turnstile-secret", config.TurnstileSecretKey)
}

func TestApplyAuthRuntimeOptionsRejectsInvalidBatchWithoutPublication(t *testing.T) {
	stable := GetAuthRuntimeConfig()
	handled, err := ApplyAuthRuntimeOptions(map[string]string{
		"RegisterEnabled":       strconv.FormatBool(!stable.RegisterEnabled),
		"TurnstileCheckEnabled": "invalid",
	})
	require.Error(t, err)
	assert.False(t, handled)
	assert.Equal(t, stable, GetAuthRuntimeConfig())
}
