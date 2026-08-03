package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Production has both:
//   - GET /api/task-submission-recovery
//   - GET /:mode/mj/task/:id/fetch  (relay midjourney mode prefix)
// Together they prevent Gin's RedirectTrailingSlash from turning
// GET /api/task into GET /api/task/. Only registering GET "/" then yields
// web NoRoute → RelayNotFound 404 (task log page toast).
func mountConflictingNeighbors(r *gin.Engine) {
	r.Group("/api/task-submission-recovery").GET("", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.Group("/:mode/mj").GET("/task/:id/fetch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
}

func hit(r http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestTaskListRouteWithoutEmptyPath404sUnderProductionNeighbors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	task := api.Group("/task")
	task.GET("/self", func(c *gin.Context) { c.String(http.StatusOK, "self") })
	task.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "list") })
	mountConflictingNeighbors(r)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	w := hit(r, "/api/task")
	assert.Equal(t, http.StatusNotFound, w.Code, "slash-only registration must reproduce production 404")

	wSlash := hit(r, "/api/task/")
	require.Equal(t, http.StatusOK, wSlash.Code)
	assert.Equal(t, "list", wSlash.Body.String())
}

func TestTaskListRouteWithEmptyPathServesAdminList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	task := api.Group("/task")
	task.GET("/self", func(c *gin.Context) { c.String(http.StatusOK, "self") })
	task.GET("", func(c *gin.Context) { c.String(http.StatusOK, "list") })
	task.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "list-slash") })
	mountConflictingNeighbors(r)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	w := hit(r, "/api/task")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "list", w.Body.String())

	wSlash := hit(r, "/api/task/")
	require.Equal(t, http.StatusOK, wSlash.Code)
	assert.Equal(t, "list-slash", wSlash.Body.String())

	wSelf := hit(r, "/api/task/self")
	require.Equal(t, http.StatusOK, wSelf.Code)
	assert.Equal(t, "self", wSelf.Body.String())
}
