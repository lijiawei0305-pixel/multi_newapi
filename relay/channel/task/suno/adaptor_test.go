package suno

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyPreservesExplicitZeroContinueAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	zero := 0.0
	tests := []struct {
		name        string
		continueAt  *float64
		wantPresent bool
	}{
		{name: "absent is omitted"},
		{name: "explicit zero is preserved", continueAt: &zero, wantPresent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("task_request", &dto.SunoSubmitReq{ContinueAt: test.continueAt})
			body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{})
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, common.DecodeJson(body, &payload))
			value, present := payload["continue_at"]
			assert.Equal(t, test.wantPresent, present)
			if test.wantPresent {
				assert.Equal(t, float64(0), value)
			}
		})
	}
}

func TestFetchTaskCancellationClosesBlackholedConnectionBeforeNextOwner(t *testing.T) {
	service.InitHttpClient()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	firstStarted := make(chan struct{})
	firstClosed := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() {
		first, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		_ = first.SetDeadline(time.Now().Add(3 * time.Second))
		firstReader := bufio.NewReader(first)
		request, readErr := http.ReadRequest(firstReader)
		if readErr != nil {
			_ = first.Close()
			serverDone <- readErr
			return
		}
		_, readErr = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		if readErr != nil {
			_ = first.Close()
			serverDone <- readErr
			return
		}
		close(firstStarted)

		oneByte := make([]byte, 1)
		_, closeErr := firstReader.Read(oneByte)
		_ = first.Close()
		firstClosed <- closeErr

		second, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer second.Close()
		_ = second.SetDeadline(time.Now().Add(3 * time.Second))
		secondRequest, readErr := http.ReadRequest(bufio.NewReader(second))
		if readErr != nil {
			serverDone <- readErr
			return
		}
		_, readErr = io.Copy(io.Discard, secondRequest.Body)
		_ = secondRequest.Body.Close()
		if readErr != nil {
			serverDone <- readErr
			return
		}
		_, writeErr := io.WriteString(second, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 28\r\nConnection: close\r\n\r\n{\"code\":\"success\",\"data\":[]}")
		serverDone <- writeErr
	}()

	adaptor := &TaskAdaptor{}
	ctx, cancel := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		resp, fetchErr := adaptor.FetchTask(ctx, "http://"+listener.Addr().String(), "test-key", map[string]any{"ids": []string{"first"}}, "")
		if resp != nil {
			_ = resp.Body.Close()
		}
		firstResult <- fetchErr
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first poll never reached the upstream blackhole")
	}
	cancel()
	select {
	case fetchErr := <-firstResult:
		require.Error(t, fetchErr)
		assert.True(t, errors.Is(fetchErr, context.Canceled), "unexpected cancellation error: %v", fetchErr)
	case <-time.After(time.Second):
		t.Fatal("canceled FetchTask did not return")
	}
	select {
	case closeErr := <-firstClosed:
		if netErr, ok := closeErr.(net.Error); ok && netErr.Timeout() {
			t.Fatalf("blackholed provider connection outlived the canceled poll context: %v", closeErr)
		}
		require.Error(t, closeErr, "the first connection unexpectedly remained reusable")
	case <-time.After(time.Second):
		t.Fatal("blackholed provider connection outlived the canceled poll context")
	}

	resp, err := adaptor.FetchTask(context.Background(), "http://"+listener.Addr().String(), "test-key", map[string]any{"ids": []string{"second"}}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	select {
	case serverErr := <-serverDone:
		require.NoError(t, serverErr)
	case <-time.After(time.Second):
		t.Fatal("replacement poll did not complete")
	}
}
