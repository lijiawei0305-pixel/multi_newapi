package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFetchOllamaModelsHonorsCallerCancellation(t *testing.T) {
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
		_, err := FetchOllamaModelsWithContext(ctx, server.URL, "")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Ollama model request did not start")
	}
	cancel()
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Ollama server did not observe cancellation")
	}
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Ollama model request did not return after cancellation")
	}
}
