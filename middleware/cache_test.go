package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCacheHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		path string
		want string
	}{
		{"/", "no-cache"},
		{"/index.html", "no-cache"},
		{"/lp-assets/dengpao_points.bin", "public, max-age=2592000"},
		{"/lp-assets/wedream-logo.png", "public, max-age=2592000"},
		{"/assets/app.js", "max-age=604800"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			r := gin.New()
			r.Use(Cache())
			r.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Header().Get("Cache-Control"), "path=%s", tc.path)
		})
	}
}
