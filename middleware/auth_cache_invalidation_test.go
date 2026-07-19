package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenAuthRejectsAfterHardDeleteAndCachesAreGone(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.UserOAuthBinding{}))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedis := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousFrequency := common.SyncFrequency
	model.DB = db
	model.LOG_DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.SyncFrequency = 60
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RDB = previousRedis
		common.RedisEnabled = previousRedisEnabled
		common.SyncFrequency = previousFrequency
		require.NoError(t, redisClient.Close())
	})

	user := model.User{
		Id:       84001,
		Username: "hard-delete-auth-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	token := model.Token{
		Id:          84002,
		UserId:      user.Id,
		Key:         "harddeleteauth",
		Name:        "hard delete auth token",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 100,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)

	userCacheKey := "user:84001"
	tokenCacheKey := "token:" + common.GenerateHMAC(token.Key)
	require.NoError(t, common.RedisHSetObj(userCacheKey, user.ToBaseUser(), time.Minute))
	cachedToken := token
	cachedToken.Clean()
	require.NoError(t, common.RedisHSetObj(tokenCacheKey, &cachedToken, time.Minute))
	require.True(t, server.Exists(userCacheKey))
	require.True(t, server.Exists(tokenCacheKey))

	require.NoError(t, model.HardDeleteUserById(user.Id))
	require.False(t, server.Exists(userCacheKey))
	require.False(t, server.Exists(tokenCacheKey))
	// Exercise the authorization fail-closed path even under a worst-case stale
	// token hash from another source: the missing user must still block access.
	require.NoError(t, common.RedisHSetObj(tokenCacheKey, &cachedToken, time.Minute))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	allowed := false
	router.GET("/v1/cache-auth", TokenAuth(), func(c *gin.Context) {
		allowed = true
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/cache-auth", nil)
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	router.ServeHTTP(recorder, request)

	assert.False(t, allowed, "an orphaned token must not authorize a hard-deleted user")
	assert.NotEqual(t, http.StatusNoContent, recorder.Code)
}
