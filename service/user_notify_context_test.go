package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestWebhookNotificationHonorsCallerCancellation(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previousSetting := *setting
	setting.EnableSSRFProtection = false
	t.Cleanup(func() {
		*setting = previousSetting
	})
	t.Setenv("NO_PROXY", "*")
	InitHttpClient()

	started := make(chan struct{})
	serverCanceled := make(chan struct{})
	releaseServer := make(chan struct{})
	defer close(releaseServer)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		close(started)
		select {
		case <-r.Context().Done():
			close(serverCanceled)
		case <-releaseServer:
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- SendWebhookNotifyWithContext(ctx, server.URL, "", dto.Notify{Type: "test", Title: "title", Content: "content"})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook request did not start")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
		require.NotContains(t, err.Error(), server.URL)
	case <-time.After(2 * time.Second):
		t.Fatal("webhook call did not return after cancellation")
	}
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook server did not observe cancellation")
	}
}
