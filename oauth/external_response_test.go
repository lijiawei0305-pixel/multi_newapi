package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLinuxDOExchangeRejectsOversizedResponseWithSafeError(t *testing.T) {
	prefix := `{"access_token":"`
	suffix := `"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		padding := int(common.ControlPlaneJSONMaxBytes+1) - len(prefix) - len(suffix)
		_, _ = w.Write([]byte(prefix + strings.Repeat("a", padding) + suffix))
	}))
	defer server.Close()
	t.Setenv("LINUX_DO_TOKEN_ENDPOINT", server.URL)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "https://gateway.example/api/oauth/linuxdo", nil)
	_, err := (&LinuxDOProvider{}).ExchangeToken(context.Background(), "authorization-code", c)
	require.Error(t, err)
	var oauthErr *OAuthError
	require.True(t, errors.As(err, &oauthErr))
	require.Equal(t, i18n.MsgOAuthTokenFailed, oauthErr.MsgKey)
	require.Equal(t, i18n.MsgOAuthTokenFailed, err.Error())
	require.NotContains(t, err.Error(), server.URL)
	require.NotContains(t, err.Error(), "authorization-code")
}
