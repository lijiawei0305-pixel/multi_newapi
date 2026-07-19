package mtwire

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/siteconfig"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type logoAssetSpy struct {
	uploaded bool
	size     int
}

func (s *logoAssetSpy) Upload(_ context.Context, _ int64, file siteconfig.File) (string, error) {
	s.uploaded = true
	s.size = len(file.Data)
	return "data:image/png;base64,dGVzdA==", nil
}

func (*logoAssetSpy) Takedown(context.Context, int64) error { return nil }

func newLogoUploadTestApp(t *testing.T) (*App, *logoAssetSpy) {
	t.Helper()
	agentRepo := agent.NewMemRepo()
	require.NoError(t, agentRepo.SetAgentType(context.Background(), 42, agent.AgentParams{Level: 1}))
	configRepo := siteconfig.NewMemRepo()
	assets := &logoAssetSpy{}
	return &App{
		AgentService: agent.NewService(agentRepo, nil),
		SiteConfig:   siteconfig.NewService(configRepo),
		Assets:       assets,
	}, assets
}

func newLogoMultipartRequest(t *testing.T, fileBytes int, extraFieldBytes int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if extraFieldBytes > 0 {
		field, err := writer.CreateFormField("padding")
		require.NoError(t, err)
		_, err = io.WriteString(field, strings.Repeat("x", extraFieldBytes))
		require.NoError(t, err)
	}
	file, err := writer.CreateFormFile("file", "logo.png")
	require.NoError(t, err)
	_, err = io.CopyN(file, zeroReader{}, int64(fileBytes))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/tenant/site-config/logo", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestHandleAgentUploadLogoBoundsEntireMultipartRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, unknownLength := range []bool{false, true} {
		name := "declared content length"
		if unknownLength {
			name = "streamed content length"
		}
		t.Run(name, func(t *testing.T) {
			app, assets := newLogoUploadTestApp(t)
			req := newLogoMultipartRequest(t, 8, int(maxAgentLogoMultipartRequestBytes))
			require.Greater(t, req.ContentLength, maxAgentLogoMultipartRequestBytes)
			if unknownLength {
				req.ContentLength = -1
			}

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = req
			c.Set(ginKeyAgentTenant, int64(42))
			app.HandleAgentUploadLogo(c)

			assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
			assert.False(t, assets.uploaded)
			assert.Contains(t, recorder.Body.String(), "ASSET_TOO_LARGE")
		})
	}
}

func TestHandleAgentUploadLogoAllowsMaximumFileWithNormalMultipartOverhead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, assets := newLogoUploadTestApp(t)
	req := newLogoMultipartRequest(t, siteconfig.MaxAssetBytes, 0)
	require.LessOrEqual(t, req.ContentLength, maxAgentLogoMultipartRequestBytes)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	c.Set(ginKeyAgentTenant, int64(42))
	app.HandleAgentUploadLogo(c)

	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, assets.uploaded)
	assert.Equal(t, siteconfig.MaxAssetBytes, assets.size)
}
