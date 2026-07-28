package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
)

func modelRateLimitTestRouter(handler gin.HandlerFunc, downstream gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 701)
		c.Next()
	})
	router.Use(handler)
	router.POST("/v1/chat/completions", downstream)
	return router
}

func runConcurrentModelRequests(t *testing.T, router http.Handler, total int) []int {
	t.Helper()
	start := make(chan struct{})
	statuses := make(chan int, total)
	var waitGroup sync.WaitGroup
	waitGroup.Add(total)
	for range total {
		go func() {
			defer waitGroup.Done()
			<-start
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			statuses <- response.Code
		}()
	}
	close(start)
	waitGroup.Wait()
	close(statuses)

	result := make([]int, 0, total)
	for status := range statuses {
		result = append(result, status)
	}
	return result
}

func assertModelSuccessConcurrencyLimit(t *testing.T, handler gin.HandlerFunc) {
	t.Helper()
	const (
		maxSuccess = 5
		total      = 100
	)
	entered := make(chan struct{}, total)
	release := make(chan struct{})
	router := modelRateLimitTestRouter(handler, func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})

	statusResult := make(chan []int, 1)
	go func() {
		statusResult <- runConcurrentModelRequests(t, router, total)
	}()
	for range maxSuccess {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for admitted requests")
		}
	}
	close(release)
	statuses := <-statusResult

	allowed, limited := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusNoContent:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Errorf("unexpected status %d", status)
		}
	}
	assert.Equal(t, maxSuccess, allowed)
	assert.Equal(t, total-maxSuccess, limited)
}

func TestModelSuccessRedisLimitConcurrentRequestsDoNotExceedMaximum(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousClient := common.RDB
	previousExpiration := common.RateLimitKeyExpirationDuration
	common.RDB = client
	common.RateLimitKeyExpirationDuration = 20 * time.Minute
	t.Cleanup(func() {
		common.RDB = previousClient
		common.RateLimitKeyExpirationDuration = previousExpiration
		_ = client.Close()
	})

	assertModelSuccessConcurrencyLimit(t, redisRateLimitHandler(10, 0, 5))
	length, err := client.LLen(context.Background(), "rateLimit:MRRLS:701").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(5), length)
}

func TestModelSuccessRedisLimitFailedRequestsReleaseReservations(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousClient := common.RDB
	previousExpiration := common.RateLimitKeyExpirationDuration
	common.RDB = client
	common.RateLimitKeyExpirationDuration = 20 * time.Minute
	t.Cleanup(func() {
		common.RDB = previousClient
		common.RateLimitKeyExpirationDuration = previousExpiration
		_ = client.Close()
	})

	handler := redisRateLimitHandler(10, 0, 3)
	failingRouter := modelRateLimitTestRouter(handler, func(c *gin.Context) {
		c.Status(http.StatusBadGateway)
	})
	for range 3 {
		response := httptest.NewRecorder()
		failingRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		assert.Equal(t, http.StatusBadGateway, response.Code)
	}
	length, err := client.LLen(context.Background(), "rateLimit:MRRLS:701").Result()
	require.NoError(t, err)
	assert.Zero(t, length)

	successRouter := modelRateLimitTestRouter(handler, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	for range 3 {
		response := httptest.NewRecorder()
		successRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		assert.Equal(t, http.StatusNoContent, response.Code)
	}
	response := httptest.NewRecorder()
	successRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
}

func TestModelSuccessMemoryLimitConcurrentRequestsDoNotExceedMaximum(t *testing.T) {
	modelSuccessMemoryReservations = modelSuccessMemoryReservationStore{}
	assertModelSuccessConcurrencyLimit(t, memoryRateLimitHandler(10, 0, 5))
}

func TestModelSuccessMemoryLimitFailedRequestsReleaseReservations(t *testing.T) {
	modelSuccessMemoryReservations = modelSuccessMemoryReservationStore{}
	handler := memoryRateLimitHandler(10, 0, 2)
	failingRouter := modelRateLimitTestRouter(handler, func(c *gin.Context) {
		c.Status(http.StatusBadRequest)
	})
	for range 2 {
		response := httptest.NewRecorder()
		failingRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		assert.Equal(t, http.StatusBadRequest, response.Code)
	}

	successRouter := modelRateLimitTestRouter(handler, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	for range 2 {
		response := httptest.NewRecorder()
		successRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		assert.Equal(t, http.StatusNoContent, response.Code)
	}
	response := httptest.NewRecorder()
	successRouter.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
}
