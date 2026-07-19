package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSyncUpstreamModelsRejectsMalformedOptionalBodyBeforeSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, body := range map[string]string{
		"truncated":         `{"overwrite":`,
		"wrong top-level":   `[]`,
		"null body":         `null`,
		"trailing garbage":  `{} trailing`,
		"second JSON value": `{} {}`,
		"oversized":         strings.Repeat(" ", int(syncRequestBodyLimit)+1),
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/models/sync_upstream", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			SyncUpstreamModels(c)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"success":false`)
		})
	}
}
