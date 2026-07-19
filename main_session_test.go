package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSessionTestEngine(secret []byte, handler gin.HandlerFunc) *gin.Engine {
	store := cookie.NewStore(secret)
	store.Options(sessionCookieOptions())
	engine := gin.New()
	engine.Use(sessions.Sessions("session", store))
	engine.GET("/session", handler)
	return engine
}

func TestProductionSessionCookieAttributesAndCrossInstanceVerification(t *testing.T) {
	originalSecure := common.SessionCookieSecure
	common.SessionCookieSecure = true
	t.Cleanup(func() { common.SessionCookieSecure = originalSecure })
	secret := []byte("shared-session-secret-with-sufficient-entropy")

	writer := newSessionTestEngine(secret, func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 42)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	writeRecorder := httptest.NewRecorder()
	writer.ServeHTTP(writeRecorder, httptest.NewRequest(http.MethodGet, "/session", nil))
	require.Equal(t, http.StatusNoContent, writeRecorder.Code)
	cookies := writeRecorder.Result().Cookies()
	require.Len(t, cookies, 1)
	sessionCookie := cookies[0]
	assert.True(t, sessionCookie.Secure)
	assert.True(t, sessionCookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)
	assert.Equal(t, 2592000, sessionCookie.MaxAge)

	reader := newSessionTestEngine(secret, func(c *gin.Context) {
		assert.Equal(t, 42, sessions.Default(c).Get("id"))
		c.Status(http.StatusNoContent)
	})
	readRequest := httptest.NewRequest(http.MethodGet, "/session", nil)
	readRequest.AddCookie(sessionCookie)
	readRecorder := httptest.NewRecorder()
	reader.ServeHTTP(readRecorder, readRequest)
	assert.Equal(t, http.StatusNoContent, readRecorder.Code)
}
