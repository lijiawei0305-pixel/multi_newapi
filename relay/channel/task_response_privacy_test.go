package channel_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/ali"
	"github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/relay/channel/task/hailuo"
	"github.com/QuantumNous/new-api/relay/channel/task/jimeng"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/vidu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskAdaptorParseErrorsDoNotRetainRawResponse(t *testing.T) {
	responseBody := []byte(`{"prompt":"private prompt","response":"private response"} trailing`)
	tests := []struct {
		name       string
		doResponse func(*http.Response) *dto.TaskError
	}{
		{name: "Ali", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&ali.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
		{name: "Doubao", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&doubao.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
		{name: "Hailuo", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&hailuo.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
		{name: "Jimeng", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&jimeng.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
		{name: "Sora", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&sora.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
		{name: "Vidu", doResponse: func(resp *http.Response) *dto.TaskError {
			_, _, err := (&vidu.TaskAdaptor{}).DoResponse(nil, resp, nil)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(bytes.NewReader(responseBody)),
			}
			taskErr := tt.doResponse(resp)
			require.NotNil(t, taskErr)
			require.Error(t, taskErr.Error)
			assert.Contains(t, taskErr.Error.Error(), common.PayloadMetadata(responseBody))
			assert.NotContains(t, taskErr.Error.Error(), "private prompt")
			assert.NotContains(t, taskErr.Error.Error(), "private response")
			assert.NotContains(t, taskErr.Message, "private prompt")
			assert.NotContains(t, taskErr.Message, "private response")
		})
	}
}
