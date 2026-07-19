package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAuthoritativeSessionTest(t *testing.T, user *model.User, auth gin.HandlerFunc, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(user).Error)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("authoritative-session-test"))))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		// These are intentionally stale and more privileged than the DB row.
		session.Set("id", user.Id)
		session.Set("username", "stale-admin")
		session.Set("role", common.RoleRootUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "stale-group")
		c.Next()
	})
	router.GET("/protected", auth, handler)
	return router
}

func performAuthoritativeSessionRequest(t *testing.T, router *gin.Engine, userId int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("New-Api-User", strconv.Itoa(userId))
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAdminAuthRejectsDemotedStaleSession(t *testing.T) {
	user := &model.User{Id: 301, Username: "demoted-user", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "current"}
	router := setupAuthoritativeSessionTest(t, user, AdminAuth(), func(c *gin.Context) {
		c.String(http.StatusNoContent, "allowed")
	})

	recorder := performAuthoritativeSessionRequest(t, router, user.Id)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "allowed")
}

func TestUserAuthRejectsDisabledStaleSession(t *testing.T) {
	user := &model.User{Id: 302, Username: "disabled-user", Password: "password", Role: common.RoleAdminUser, Status: common.UserStatusDisabled, Group: "current"}
	router := setupAuthoritativeSessionTest(t, user, UserAuth(), func(c *gin.Context) {
		c.String(http.StatusNoContent, "allowed")
	})

	recorder := performAuthoritativeSessionRequest(t, router, user.Id)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "allowed")
}

func TestUserAuthUsesCurrentDatabaseAuthorizationContext(t *testing.T) {
	user := &model.User{Id: 303, Username: "current-user", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "current-group"}
	router := setupAuthoritativeSessionTest(t, user, UserAuth(), func(c *gin.Context) {
		c.String(http.StatusOK, "%s:%d", c.GetString("group"), c.GetInt("role"))
	})

	recorder := performAuthoritativeSessionRequest(t, router, user.Id)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "current-group:1", recorder.Body.String())
}
