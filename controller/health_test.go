package controller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	paymentmigrate "github.com/QuantumNous/new-api/internal/payment/migrate"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func runHealthHandler(t *testing.T, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler(ctx)
	return recorder
}

func decodeHealthResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestHealthLiveDoesNotDependOnBackends(t *testing.T) {
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalRedis := common.RDB
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedis
	})

	model.DB = nil
	common.RedisEnabled = true
	common.RDB = nil

	recorder := runHealthHandler(t, HealthLive)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "ok", decodeHealthResponse(t, recorder)["status"])
}

func TestHealthReadyChecksCurrentDatabaseAndConfiguredRedis(t *testing.T) {
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalRedis := common.RDB
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedis
	})

	db, err := gorm.Open(sqlite.Open("file:health-ready?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	// payment schema readiness 要求 CurrentVersion >= SchemaVersion
	require.NoError(t, paymentmigrate.EnsureSchema(context.Background(), db))
	model.DB = db
	common.RedisEnabled = false
	common.RDB = nil

	recorder := runHealthHandler(t, HealthReady)
	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeHealthResponse(t, recorder)
	assert.Equal(t, "ok", payload["status"])
	checks := payload["checks"].(map[string]any)
	assert.Equal(t, "ok", checks["database"])
	assert.Equal(t, "disabled", checks["redis"])
	assert.Contains(t, fmt.Sprint(checks["payment_schema"]), "ok")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	common.RedisEnabled = true
	redisClient := redis.NewClient(&redis.Options{
		Addr:       closedAddress,
		MaxRetries: 0,
	})
	common.RDB = redisClient
	t.Cleanup(func() { _ = redisClient.Close() })

	recorder = runHealthHandler(t, HealthReady)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	payload = decodeHealthResponse(t, recorder)
	assert.Equal(t, "unavailable", payload["status"])
	assert.Equal(t, "unavailable", payload["checks"].(map[string]any)["redis"])

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	common.RedisEnabled = false
	common.RDB = nil

	recorder = runHealthHandler(t, HealthReady)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	payload = decodeHealthResponse(t, recorder)
	assert.Equal(t, "unavailable", payload["checks"].(map[string]any)["database"])
	assert.NotContains(t, fmt.Sprint(payload), "sql: database is closed")
}
