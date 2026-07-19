package hailuo

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultContextCancelsFileRetrieval(t *testing.T) {
	service.InitHttpClient()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	requestStarted := make(chan struct{})
	connectionClosed := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			connectionClosed <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(conn)
		if _, readErr := http.ReadRequest(reader); readErr != nil {
			connectionClosed <- readErr
			return
		}
		close(requestStarted)
		oneByte := make([]byte, 1)
		_, closeErr := reader.Read(oneByte)
		connectionClosed <- closeErr
	}()

	pollBody, err := common.Marshal(QueryTaskResponse{
		TaskID:   "provider-task",
		Status:   TaskStatusSuccess,
		FileID:   "provider-file",
		BaseResp: BaseResp{StatusCode: StatusSuccess},
	})
	require.NoError(t, err)
	adaptor := &TaskAdaptor{apiKey: "secret", baseURL: "http://" + listener.Addr().String()}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, parseErr := adaptor.ParseTaskResultContext(ctx, pollBody)
		result <- parseErr
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Hailuo file retrieval did not start")
	}
	cancel()
	select {
	case parseErr := <-result:
		require.Error(t, parseErr)
		assert.True(t, errors.Is(parseErr, context.Canceled), "unexpected cancellation error: %v", parseErr)
	case <-time.After(time.Second):
		t.Fatal("Hailuo file retrieval ignored poll cancellation")
	}
	select {
	case closeErr := <-connectionClosed:
		if netErr, ok := closeErr.(net.Error); ok && netErr.Timeout() {
			t.Fatalf("Hailuo connection outlived poll cancellation: %v", closeErr)
		}
		require.Error(t, closeErr)
	case <-time.After(time.Second):
		t.Fatal("Hailuo connection outlived poll cancellation")
	}
}
