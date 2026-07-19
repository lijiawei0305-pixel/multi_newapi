package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func useFetchPolicyForTest(t *testing.T, setting system_setting.FetchSetting) {
	t.Helper()
	current := system_setting.GetFetchSetting()
	previous := *current
	*current = setting
	t.Cleanup(func() {
		*current = previous
	})
}

func TestValidateControlPlaneURLBlocksPrivateAddress(t *testing.T) {
	useFetchPolicyForTest(t, system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         false,
		AllowedPorts:           []string{"80", "443", "8080", "8443"},
		ApplyIPFilterForDomain: true,
	})
	require.Error(t, validateControlPlaneURL("http://127.0.0.1:8080/.well-known/openid-configuration"))
	require.Error(t, validateControlPlaneURL("http://[::1]:8080/v1/models"))
}

func TestFetchModelsFromEndpointHonorsCancellation(t *testing.T) {
	useFetchPolicyForTest(t, system_setting.FetchSetting{EnableSSRFProtection: false})
	started := make(chan struct{})
	serverCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(serverCanceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := fetchModelsFromEndpoint(ctx, server.Client(), server.URL, 0, "secret")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model request did not start")
	}
	cancel()
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("model endpoint did not observe cancellation")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("model fetch did not return after cancellation")
	}
}

func TestFetchModelsFromEndpointControlPlaneLimit(t *testing.T) {
	useFetchPolicyForTest(t, system_setting.FetchSetting{EnableSSRFProtection: false})
	prefix := `{"data":[{"id":"`
	suffix := `"}]}`
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		size := common.ControlPlaneJSONMaxBytes
		if requestCount == 2 {
			size++
		}
		padding := int(size) - len(prefix) - len(suffix)
		_, _ = w.Write([]byte(prefix + strings.Repeat("a", padding) + suffix))
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(context.Background(), server.Client(), server.URL, 0, "")
	require.NoError(t, err)
	require.Len(t, models, 1)
	_, err = fetchModelsFromEndpoint(context.Background(), server.Client(), server.URL, constant.ChannelTypeOpenAI, "")
	require.EqualError(t, err, "model fetch response is invalid")
}
