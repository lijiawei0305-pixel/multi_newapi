package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageURLHelpersCancelSlowDownloads(t *testing.T) {
	originalHTTPClient, originalProtectedClient := httpClient, ssrfProtectedHTTPClient
	originalMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	InitHttpClient()
	t.Cleanup(func() {
		httpClient = originalHTTPClient
		ssrfProtectedHTTPClient = originalProtectedClient
		constant.MaxFileDownloadMB = originalMaxFileDownloadMB
	})
	fetchSetting := system_setting.GetFetchSetting()
	original := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = original })
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "image base64 conversion",
			run: func(ctx context.Context) error {
				_, _, err := GetImageFromURLContext(ctx, server.URL)
				return err
			},
		},
		{
			name: "image header decoding",
			run: func(ctx context.Context) error {
				_, _, err := DecodeURLImageDataContext(ctx, server.URL)
				return err
			},
		},
		{
			name: "cached file loading",
			run: func(ctx context.Context) error {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
				_, err := loadFromURL(c, server.URL)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			timer := time.AfterFunc(50*time.Millisecond, cancel)
			defer timer.Stop()
			started := time.Now()

			err := test.run(ctx)

			require.Error(t, err)
			assert.True(t, errors.Is(err, context.Canceled), err)
			assert.Less(t, time.Since(started), time.Second, "client cancellation must release a post-ACK response slot promptly")
		})
	}
}
