package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestGetResponseBodyWithContextCancelsChannelControlPlaneRequest(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	// Local httptest server binds 127.0.0.1; disable SSRF for cancellation semantics under test.
	// Production still validates via validateControlPlaneURL + SSRF-protected client.
	useFetchPolicyForTest(t, system_setting.FetchSetting{EnableSSRFProtection: false})
	service.InitHttpClient()
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
		_, err := GetResponseBodyWithContext(ctx, http.MethodGet, server.URL, &model.Channel{}, http.Header{})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("channel control-plane request did not start")
	}
	cancel()
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("channel control-plane server did not observe cancellation")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("channel control-plane request did not return after cancellation")
	}
}
