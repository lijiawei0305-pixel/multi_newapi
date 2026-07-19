package oauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericOAuthLogsMetadataWithoutCredentialsOrProfile(t *testing.T) {
	useOAuthFetchSetting(t, system_setting.FetchSetting{EnableSSRFProtection: false})

	tokenBody := `{"access_token":"private-access-token","refresh_token":"private-refresh-token","id_token":"private-id-token","token_type":"Bearer","scope":"openid profile"}`
	userBody := `{"sub":"private-provider-id","preferred_username":"private-user","name":"Private Name","email":"private@example.test"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(tokenBody))
		case "/user":
			_, _ = w.Write([]byte(userBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	originalDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() { common.DebugEnabled = originalDebug })
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:             "Test Provider",
		Slug:             "test-provider",
		ClientId:         "private-client-id",
		ClientSecret:     "private-client-secret",
		TokenEndpoint:    server.URL + "/token?credential=private-endpoint-secret",
		UserInfoEndpoint: server.URL + "/user?credential=private-user-endpoint-secret",
		AuthStyle:        AuthStyleInParams,
		UserIdField:      "sub",
		UsernameField:    "preferred_username",
		DisplayNameField: "name",
		EmailField:       "email",
	})
	authorizationCode := "private-authorization-code"

	token, err := provider.ExchangeToken(context.Background(), authorizationCode, nil)
	require.NoError(t, err)
	require.Equal(t, "private-access-token", token.AccessToken)
	user, err := provider.GetUserInfo(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "private-provider-id", user.ProviderUserID)

	logged := output.String()
	assert.Contains(t, logged, logger.PayloadMetadata([]byte(authorizationCode)))
	assert.Contains(t, logged, logger.PayloadMetadata([]byte(tokenBody)))
	assert.Contains(t, logged, logger.PayloadMetadata([]byte(userBody)))
	for _, secret := range []string{
		"private-authorization-code",
		"private-client-id",
		"private-client-secret",
		"private-endpoint-secret",
		"private-user-endpoint-secret",
		"private-access-token",
		"private-refresh-token",
		"private-id-token",
		"private-provider-id",
		"private-user",
		"Private Name",
		"private@example.test",
	} {
		assert.NotContains(t, logged, secret)
	}
}

func TestGenericOAuthMalformedFormReturnsParseErrorWithoutBody(t *testing.T) {
	useOAuthFetchSetting(t, system_setting.FetchSetting{EnableSSRFProtection: false})

	const malformedBody = `%zz=oauth-parse-canary`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(malformedBody))
	}))
	t.Cleanup(server.Close)

	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:          "Malformed Provider",
		Slug:          "malformed-provider",
		ClientId:      "client-id",
		ClientSecret:  "client-secret",
		TokenEndpoint: server.URL,
		AuthStyle:     AuthStyleInParams,
	})
	_, err := provider.ExchangeToken(context.Background(), "authorization-code", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL escape")
	assert.Contains(t, output.String(), logger.PayloadMetadata([]byte(malformedBody)))
	assert.NotContains(t, output.String(), "oauth-parse-canary")
}
