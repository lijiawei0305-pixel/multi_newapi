package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useOAuthFetchSetting(t *testing.T, fetchSetting system_setting.FetchSetting) {
	t.Helper()
	settings := system_setting.GetFetchSetting()
	original := *settings
	*settings = fetchSetting
	service.InitHttpClient()
	t.Cleanup(func() {
		*settings = original
		service.InitHttpClient()
	})
}

func privateOAuthTarget(t *testing.T, calls *atomic.Int32) (string, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"unexpected","sub":"unexpected","email":"unexpected@example.test"}`))
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	return server.URL, parsed.Port()
}

func TestOAuthRuntimeRequestsRejectPrivateEndpoints(t *testing.T) {
	var calls atomic.Int32
	endpoint, port := privateOAuthTarget(t, &calls)
	useOAuthFetchSetting(t, system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         false,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{port},
		ApplyIPFilterForDomain: true,
	})

	oidcSettings := system_setting.GetOIDCSettings()
	originalOIDC := *oidcSettings
	t.Cleanup(func() { *oidcSettings = originalOIDC })

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "generic token exchange",
			run: func() error {
				provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
					Name:          "Private Generic",
					Slug:          "private-generic",
					ClientId:      "client-id",
					ClientSecret:  "client-secret",
					TokenEndpoint: endpoint + "/token",
					AuthStyle:     AuthStyleInParams,
				})
				_, err := provider.ExchangeToken(context.Background(), "authorization-code", nil)
				return err
			},
		},
		{
			name: "generic user info",
			run: func() error {
				provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
					Name:             "Private Generic",
					Slug:             "private-generic",
					UserInfoEndpoint: endpoint + "/userinfo",
				})
				_, err := provider.GetUserInfo(context.Background(), &OAuthToken{AccessToken: "access-token"})
				return err
			},
		},
		{
			name: "OIDC token exchange",
			run: func() error {
				oidcSettings.TokenEndpoint = endpoint + "/token"
				_, err := (&OIDCProvider{}).ExchangeToken(context.Background(), "authorization-code", nil)
				return err
			},
		},
		{
			name: "OIDC user info",
			run: func() error {
				oidcSettings.UserInfoEndpoint = endpoint + "/userinfo"
				_, err := (&OIDCProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "access-token"})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls.Store(0)
			err := test.run()
			require.Error(t, err)
			var oauthErr *OAuthError
			require.ErrorAs(t, err, &oauthErr)
			assert.Equal(t, i18n.MsgOAuthConnectFailed, oauthErr.MsgKey)
			assert.Zero(t, calls.Load(), "blocked endpoint must not receive a request")
		})
	}
}

func TestGenericOAuthRedirectCannotEscapeToUnapprovedAddress(t *testing.T) {
	var sourceCalls atomic.Int32
	var targetCalls atomic.Int32
	redirectURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/private" {
			targetCalls.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"must-not-be-reached"}`))
			return
		}
		sourceCalls.Add(1)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	redirectURL = "http://localhost:" + parsed.Port() + "/private"

	useOAuthFetchSetting(t, system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		DomainList:             []string{"localhost"},
		IpFilterMode:           true,
		IpList:                 []string{"127.0.0.1/32"},
		AllowedPorts:           []string{parsed.Port()},
		ApplyIPFilterForDomain: true,
	})

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:          "Redirect Generic",
		Slug:          "redirect-generic",
		ClientId:      "client-id",
		ClientSecret:  "client-secret",
		TokenEndpoint: server.URL + "/token",
		AuthStyle:     AuthStyleInParams,
	})
	_, err = provider.ExchangeToken(context.Background(), "authorization-code", nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), sourceCalls.Load())
	assert.Zero(t, targetCalls.Load(), "blocked redirect target must not receive a request")
}
