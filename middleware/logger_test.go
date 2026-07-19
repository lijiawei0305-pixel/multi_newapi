package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type concurrentLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *concurrentLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *concurrentLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func TestSetUpLoggerUsesCurrentSynchronizedWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := &concurrentLogBuffer{}
	second := &concurrentLogBuffer{}

	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	gin.DefaultWriter = first
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	engine := gin.New()
	SetUpLogger(engine)
	engine.GET("/health", func(c *gin.Context) {
		c.Set(common.RequestIdKey, "request-123")
		c.Set(RouteTagKey, "health")
		c.Status(http.StatusNoContent)
	})

	common.LogWriterMu.Lock()
	gin.DefaultWriter = second
	common.LogWriterMu.Unlock()

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health?signature=raw-secret&buyer=private-user", nil))

	assert.Empty(t, first.String())
	assert.Contains(t, second.String(), "[GIN]")
	assert.Contains(t, second.String(), "health")
	assert.Contains(t, second.String(), "request-123")
	assert.Contains(t, second.String(), "GET /health")
	assert.NotContains(t, second.String(), "raw-secret")
	assert.NotContains(t, second.String(), "private-user")
	assert.NotContains(t, second.String(), "signature=")
}

func TestSynchronizedLogWriterHandlesConcurrentWriterSwaps(t *testing.T) {
	first := &concurrentLogBuffer{}
	second := &concurrentLogBuffer{}
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	gin.DefaultWriter = first
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	writer := common.SynchronizedLogWriter(false)
	payload := []byte("access-log-entry\n")
	const (
		goroutines = 16
		writes     = 100
	)
	var wg sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for write := 0; write < writes; write++ {
				_, _ = writer.Write(payload)
			}
		}()
	}
	for swap := 0; swap < writes; swap++ {
		common.LogWriterMu.Lock()
		if swap%2 == 0 {
			gin.DefaultWriter = second
		} else {
			gin.DefaultWriter = first
		}
		common.LogWriterMu.Unlock()
	}
	wg.Wait()

	totalBytes := len(first.String()) + len(second.String())
	assert.Equal(t, goroutines*writes*len(payload), totalBytes)
}
