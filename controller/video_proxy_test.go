package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteVideoDataURLAcceptsOnlyVideoMediaTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := base64.StdEncoding.EncodeToString([]byte("video-bytes"))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	require.NoError(t, writeVideoDataURL(c, "data:video/mp4;base64,"+payload))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store, private", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "video-bytes", recorder.Body.String())

	for _, contentType := range []string{"text/html", "image/svg+xml", "video/mp4\r\nX-Injected: true"} {
		t.Run(contentType, func(t *testing.T) {
			rejectedRecorder := httptest.NewRecorder()
			rejected, _ := gin.CreateTestContext(rejectedRecorder)
			err := writeVideoDataURL(rejected, "data:"+contentType+";base64,"+payload)
			require.Error(t, err)
			assert.Empty(t, rejectedRecorder.Body.String())
			assert.Empty(t, rejectedRecorder.Header().Get("Content-Type"))
		})
	}
}
