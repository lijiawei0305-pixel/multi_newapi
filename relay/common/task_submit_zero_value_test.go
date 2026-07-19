package common

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsoncommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTaskSubmitReqPreservesDurationPresence(t *testing.T) {
	for _, test := range []struct {
		name      string
		body      string
		wantValue *int
	}{
		{name: "absent", body: `{"prompt":"hello","model":"video"}`},
		{name: "numeric zero", body: `{"prompt":"hello","model":"video","duration":0}`, wantValue: jsoncommon.GetPointer(0)},
		{name: "string zero", body: `{"prompt":"hello","model":"video","duration":"0"}`, wantValue: jsoncommon.GetPointer(0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request TaskSubmitReq
			require.NoError(t, jsoncommon.Unmarshal([]byte(test.body), &request))
			if test.wantValue == nil {
				assert.Nil(t, request.Duration)
			} else {
				require.NotNil(t, request.Duration)
				assert.Equal(t, *test.wantValue, *request.Duration)
			}

			encoded, err := jsoncommon.Marshal(request)
			require.NoError(t, err)
			assert.Equal(t, test.wantValue != nil, gjson.GetBytes(encoded, "duration").Exists())
		})
	}
}

func TestTaskDurationExplicitZeroIsRejectedInsteadOfDefaulted(t *testing.T) {
	err := validateTaskDuration(TaskSubmitReq{Duration: jsoncommon.GetPointer(0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")

	err = validateTaskDuration(TaskSubmitReq{Seconds: "0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")

	assert.NoError(t, validateTaskDuration(TaskSubmitReq{}))
}

func TestTaskMetadataInvalidZeroQuantitiesAreRejectedBeforeBilling(t *testing.T) {
	for _, metadata := range []map[string]any{
		{"duration": 0},
		{"durationSeconds": float64(0)},
		{"frames": "0"},
		{"parameters": map[string]any{"duration": 0}},
	} {
		err := validateTaskDuration(TaskSubmitReq{Metadata: metadata})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "greater than zero")
	}
	assert.NoError(t, validateTaskDuration(TaskSubmitReq{Metadata: map[string]any{
		"seed": 0, "watermark": false, "parameters": map[string]any{"duration": 5},
	}}))
}

func TestTaskValidatorsRejectExplicitZeroDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, validate := range []struct {
		name string
		fn   func(*gin.Context, *RelayInfo) *dto.TaskError
	}{
		{name: "basic", fn: func(c *gin.Context, info *RelayInfo) *dto.TaskError {
			return ValidateBasicTaskRequest(c, info, "generate")
		}},
		{name: "multipart direct", fn: ValidateMultipartDirect},
	} {
		t.Run(validate.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
				"model":"video","prompt":"hello","duration":0
			}`))
			c.Request.Header.Set("Content-Type", "application/json")
			taskErr := validate.fn(c, &RelayInfo{})
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_duration", taskErr.Code)
		})
	}
}

func TestMultipartTaskParsingPreservesZeroAndFalsePresence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "hello"))
	require.NoError(t, writer.WriteField("model", "video"))
	require.NoError(t, writer.WriteField("seconds", "0"))
	require.NoError(t, writer.WriteField("bgm", "false"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	request, err := validateMultipartTaskRequest(c, &RelayInfo{}, "generate")
	require.NoError(t, err)
	require.NotNil(t, request.Duration)
	assert.Zero(t, *request.Duration)
	assert.Equal(t, false, request.Metadata["bgm"])
}
