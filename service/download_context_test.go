package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedFileDownloadContextDefaultsToThirtySeconds(t *testing.T) {
	t.Setenv("RELAY_FILE_DOWNLOAD_TIMEOUT_SECONDS", "0")
	ctx, cancel := boundedFileDownloadContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.InDelta(t, 30, time.Until(deadline).Seconds(), 1)
}

func TestBoundedFileDownloadContextHonorsShorterParentDeadline(t *testing.T) {
	t.Setenv("RELAY_FILE_DOWNLOAD_TIMEOUT_SECONDS", "30")
	parent, parentCancel := context.WithTimeout(context.Background(), time.Second)
	defer parentCancel()
	ctx, cancel := boundedFileDownloadContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.InDelta(t, 1, time.Until(deadline).Seconds(), 0.25)
}

func configureDirectDownloadContextTest(t *testing.T) {
	t.Helper()
	originalHTTPClient, originalProtectedClient := httpClient, ssrfProtectedHTTPClient
	InitHttpClient()
	t.Cleanup(func() {
		httpClient = originalHTTPClient
		ssrfProtectedHTTPClient = originalProtectedClient
	})
	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = originalFetchSetting })
}

func TestDoDownloadRequestBoundsSlowResponseBody(t *testing.T) {
	configureDirectDownloadContextTest(t)
	t.Setenv("RELAY_FILE_DOWNLOAD_TIMEOUT_SECONDS", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	resp, err := DoDownloadRequest(server.URL)
	require.NoError(t, err)
	started := time.Now()
	_, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	require.Error(t, readErr)
	assert.Less(t, time.Since(started), 2*time.Second)
}

func TestDoDownloadRequestBodyCloseCancelsDeadlineContext(t *testing.T) {
	configureDirectDownloadContextTest(t)
	t.Setenv("RELAY_FILE_DOWNLOAD_TIMEOUT_SECONDS", "30")
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	resp, err := DoDownloadRequest(server.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("closing the public download response did not cancel its deadline context")
	}
}
