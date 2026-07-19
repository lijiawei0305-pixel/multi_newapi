package minimax

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGetRequestURLForImageGeneration(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.minimax.chat",
		},
	}

	got, err := GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}

	want := "https://api.minimax.chat/v1/image_generation"
	if got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestConvertImageRequest(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesGenerations,
		OriginModelName: "image-01",
	}
	request := dto.ImageRequest{
		Model:          "image-01",
		Prompt:         "a red fox in snowfall",
		Size:           "1536x1024",
		ResponseFormat: "url",
		N:              uintPtr(2),
	}

	got, err := adaptor.ConvertImageRequest(gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New()), info, request)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	if payload["model"] != "image-01" {
		t.Fatalf("model = %#v, want %q", payload["model"], "image-01")
	}
	if payload["prompt"] != request.Prompt {
		t.Fatalf("prompt = %#v, want %q", payload["prompt"], request.Prompt)
	}
	if payload["n"] != float64(2) {
		t.Fatalf("n = %#v, want 2", payload["n"])
	}
	if payload["aspect_ratio"] != "3:2" {
		t.Fatalf("aspect_ratio = %#v, want %q", payload["aspect_ratio"], "3:2")
	}
	if payload["response_format"] != "url" {
		t.Fatalf("response_format = %#v, want %q", payload["response_format"], "url")
	}
}

func TestDoResponseForImageGeneration(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		StartTime: time.Unix(1700000000, 0),
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       httptest.NewRecorder().Result().Body,
	}
	resp.Body = ioNopCloser(`{"data":{"image_urls":["https://example.com/minimax.png"]}}`)

	adaptor := &Adaptor{}
	usage, err := adaptor.DoResponse(c, resp, info)
	if err != nil {
		t.Fatalf("DoResponse returned error: %v", err)
	}
	if usage == nil {
		t.Fatalf("DoResponse returned nil usage")
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"url":"https://example.com/minimax.png"`) {
		t.Fatalf("response body = %s, want OpenAI image response with image URL", body)
	}
	if strings.Contains(body, `"image_urls"`) {
		t.Fatalf("response body = %s, should not expose raw MiniMax image_urls payload", body)
	}
}

func TestMiniMaxImageStructured200ErrorRemainsUnacceptedRejection(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"data":{"image_urls":[],"image_base64":[]},
			"base_resp":{"status_code":1008,"status_msg":"image rejected"}
		}`)),
	}
	info := &relaycommon.RelayInfo{StartTime: time.Now()}

	usage, apiErr := miniMaxImageHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.False(t, service.IsUpstreamAccepted(c))
	require.False(t, types.IsSkipRetryError(apiErr))
}

func TestMiniMaxImageEmptySuccessIsAcceptedDeliveryFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"data":{"image_urls":[],"image_base64":[]},"base_resp":{"status_code":0}}`)),
	}

	usage, apiErr := miniMaxImageHandler(c, resp, &relaycommon.RelayInfo{StartTime: time.Now()})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}

type trackingMiniMaxBody struct {
	io.Reader
	closed bool
}

func (b *trackingMiniMaxBody) Close() error {
	b.closed = true
	return nil
}

func TestMiniMaxImageClosesBodyWhenBoundedReadFails(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &trackingMiniMaxBody{Reader: strings.NewReader(`{"data":{}}`)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	usage, apiErr := miniMaxImageHandler(c, resp, &relaycommon.RelayInfo{StartTime: time.Now()})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, body.closed)
	assert.True(t, service.IsUpstreamAccepted(c))
}

func TestMiniMaxChatClosesBodyWhenBoundedReadFails(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &trackingMiniMaxBody{Reader: strings.NewReader(`{"choices":[]}`)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	usage, apiErr := handleChatCompletionResponse(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, body.closed)
}

func TestMiniMaxImagesShareOneCumulativeEncodedBudget(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1000")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "300")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	first := strings.Repeat("A", 100)
	second := strings.Repeat("B", 100)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"data":{"image_base64":["` + first + `","` + second + `"]},"base_resp":{"status_code":0}}`,
		)),
	}

	usage, apiErr := miniMaxImageHandler(c, resp, &relaycommon.RelayInfo{StartTime: time.Now()})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}

type nopReadCloser struct {
	*strings.Reader
}

func (n nopReadCloser) Close() error {
	return nil
}

func ioNopCloser(body string) nopReadCloser {
	return nopReadCloser{Reader: strings.NewReader(body)}
}

func uintPtr(v uint) *uint {
	return &v
}
