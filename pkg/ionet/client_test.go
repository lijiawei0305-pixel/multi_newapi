package ionet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPClientResponseLimitBoundary(t *testing.T) {
	tests := []struct {
		name      string
		bodyBytes int64
		wantLimit bool
	}{
		{name: "exact limit", bodyBytes: common.ControlPlaneJSONMaxBytes},
		{name: "one byte over", bodyBytes: common.ControlPlaneJSONMaxBytes + 1, wantLimit: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = strings.NewReader(strings.Repeat("x", int(test.bodyBytes))).WriteTo(w)
			}))
			defer server.Close()

			client := NewDefaultHTTPClient(5 * time.Second)
			response, err := client.Do(&HTTPRequest{Method: http.MethodGet, URL: server.URL})
			if test.wantLimit {
				require.Error(t, err)
				require.True(t, errors.Is(err, common.ErrReadLimitExceeded))
				return
			}
			require.NoError(t, err)
			require.Len(t, response.Body, int(test.bodyBytes))
		})
	}
}

func TestDefaultHTTPClientHonorsRequestContext(t *testing.T) {
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
		_, err := NewDefaultHTTPClient(5 * time.Second).Do(&HTTPRequest{
			Context: ctx,
			Method:  http.MethodGet,
			URL:     server.URL,
		})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()

	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe context cancellation")
	}
	select {
	case err := <-result:
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(2 * time.Second):
		t.Fatal("client did not return after context cancellation")
	}
}
