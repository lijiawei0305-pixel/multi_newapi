package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskSubmissionAdminRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.TaskSubmissionRecovery{}))
	require.NoError(t, model.EnsureTaskSubmissionIdempotencyUniqueIndex(db))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})
	require.NoError(t, db.Create(&model.User{Id: 7401, Username: "ordinary", AffCode: "ordinary-7401", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.User{Id: 7402, Username: "administrator", AffCode: "admin-7402", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	const requestId = "admin-visible-uncertain"
	_, err = model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindTask))
	require.NoError(t, db.Model(&model.TaskSubmissionRecovery{}).Where("request_id = ?", requestId).Updates(map[string]interface{}{
		"last_error": "postgres://private:secret@database.internal", "payload": "private-payload", "payload_hash": strings.Repeat("a", 64),
	}).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("task-submission-admin-test"))))
	router.Use(func(c *gin.Context) {
		id, _ := strconv.Atoi(c.GetHeader("X-Test-Session-User"))
		session := sessions.Default(c)
		session.Set("id", id)
		c.Next()
	})
	router.GET("/recoveries", middleware.AdminAuth(), AdminListTaskSubmissionRecoveries)
	router.POST("/recoveries/:request_id/resolve", middleware.AdminAuth(), AdminResolveTaskSubmissionRecovery)
	return router, requestId
}

func taskSubmissionAdminRequest(router *gin.Engine, method string, path string, userId int, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Session-User", strconv.Itoa(userId))
	request.Header.Set("New-Api-User", strconv.Itoa(userId))
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestTaskSubmissionRecoveryAdminAPIEnforcesRoleAndMinimalDisclosure(t *testing.T) {
	router, requestId := setupTaskSubmissionAdminRouter(t)
	ordinary := taskSubmissionAdminRequest(router, http.MethodPost,
		"/recoveries/"+requestId+"/resolve?kind=task", 7401, `{"outcome":"unknown"}`)
	assert.NotContains(t, ordinary.Body.String(), `"success":true`)
	row, err := model.GetTaskSubmissionRecovery(requestId, model.TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, model.TaskSubmissionStatusUncertain, row.Status)

	admin := taskSubmissionAdminRequest(router, http.MethodGet, "/recoveries?status=uncertain&kind=task&limit=10", 7402, "")
	assert.Equal(t, http.StatusOK, admin.Code)
	assert.Contains(t, admin.Body.String(), requestId)
	assert.NotContains(t, admin.Body.String(), "private-payload")
	assert.NotContains(t, admin.Body.String(), "private:secret")
	assert.NotContains(t, admin.Body.String(), "payload_hash")
	assert.NotContains(t, admin.Body.String(), "idempotency_fingerprint")
}
