package channel_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	ali "github.com/QuantumNous/new-api/relay/channel/task/ali"
	doubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	gemini "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	hailuo "github.com/QuantumNous/new-api/relay/channel/task/hailuo"
	jimeng "github.com/QuantumNous/new-api/relay/channel/task/jimeng"
	kling "github.com/QuantumNous/new-api/relay/channel/task/kling"
	sora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	suno "github.com/QuantumNous/new-api/relay/channel/task/suno"
	vertex "github.com/QuantumNous/new-api/relay/channel/task/vertex"
	vidu "github.com/QuantumNous/new-api/relay/channel/task/vidu"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTaskResponseRead = errors.New("synthetic task response read failure")

type failingTaskResponseBody struct {
	closed atomic.Bool
}

func (b *failingTaskResponseBody) Read([]byte) (int, error) {
	return 0, errTaskResponseRead
}

func (b *failingTaskResponseBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestTaskAdaptorsCloseResponseBodyOnReadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		doResponse func(*gin.Context, *http.Response, *relaycommon.RelayInfo) (string, []byte, *dto.TaskError)
	}{
		{name: "ali", doResponse: (&ali.TaskAdaptor{}).DoResponse},
		{name: "doubao", doResponse: (&doubao.TaskAdaptor{}).DoResponse},
		{name: "gemini", doResponse: (&gemini.TaskAdaptor{}).DoResponse},
		{name: "hailuo", doResponse: (&hailuo.TaskAdaptor{}).DoResponse},
		{name: "jimeng", doResponse: (&jimeng.TaskAdaptor{}).DoResponse},
		{name: "kling", doResponse: (&kling.TaskAdaptor{}).DoResponse},
		{name: "sora", doResponse: (&sora.TaskAdaptor{}).DoResponse},
		{name: "suno", doResponse: (&suno.TaskAdaptor{}).DoResponse},
		{name: "vertex", doResponse: (&vertex.TaskAdaptor{}).DoResponse},
		{name: "vidu", doResponse: (&vidu.TaskAdaptor{}).DoResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := &failingTaskResponseBody{}
			resp := &http.Response{StatusCode: http.StatusOK, Body: body}

			_, _, taskErr := test.doResponse(c, resp, &relaycommon.RelayInfo{})

			require.NotNil(t, taskErr)
			assert.ErrorIs(t, taskErr.Error, errTaskResponseRead)
			assert.True(t, body.closed.Load(), "task adaptor must close response body after read failure")
		})
	}
}
