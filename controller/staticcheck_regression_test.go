package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetSubscriptionDoesNotOverwriteRemainQuotaError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalDisplayTokenStatEnabled := common.DisplayTokenStatEnabled
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.DisplayTokenStatEnabled = originalDisplayTokenStatEnabled
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	})

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE users (id integer primary key, used_quota integer)").Error)

	model.DB = db
	common.RedisEnabled = false
	common.DisplayTokenStatEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/dashboard/billing/subscription", nil)

	GetSubscription(ctx)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Contains(t, response, "error")
	assert.NotContains(t, response, "hard_limit_usd")
}

func TestResetPasswordRejectsMultipleJSONValuesWithoutConsumingCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	email := "staticcheck-reset@example.com"
	code := "valid-reset-code"
	common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(email, common.PasswordResetPurpose) })

	body := fmt.Sprintf(`{"email":%q,"token":%q} {"unexpected":true}`, email, code)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(body))

	ResetPassword(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "无效的参数", response.Message)
	assert.True(t, common.VerifyCodeWithKey(email, code, common.PasswordResetPurpose))
}
