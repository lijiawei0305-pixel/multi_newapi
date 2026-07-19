package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// OpenaiImageHandler handles non-streaming OpenAI image responses
// (generations/edits), returning the parsed usage for billing.
func OpenaiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
		service.CopyUpstreamResponseHeaders(c, buffered.Header(), resp.Header)
		buffered.WriteHeader(resp.StatusCode)
		if _, err := common.CopyWithLimit(buffered, resp.Body, common.UpstreamJSONBodyLimit()); err != nil {
			service.MarkUpstreamAccepted(c)
			_ = buffered.Fail(err)
			common.SysError(fmt.Sprintf("accepted OpenAI image response spool failed: error_type=%T", err))
			return nil, channel.AcceptedResponseDeliveryError()
		}

		var usageResp dto.SimpleResponse
		if err := buffered.InspectBody(func(reader io.ReadSeeker) error {
			return common.DecodeJson(reader, &usageResp)
		}); err != nil {
			service.MarkUpstreamAccepted(c)
			_ = buffered.Fail(err)
			common.SysError(fmt.Sprintf("accepted OpenAI image response decode failed: error_type=%T", err))
			return nil, channel.AcceptedResponseDeliveryError()
		}
		if oaiError := usageResp.GetOpenAIError(); meaningfulOpenAIError(oaiError) {
			_ = buffered.Fail(errors.New("accepted image response contained a provider error"))
			return nil, types.WithOpenAIError(*oaiError, semanticUpstreamErrorStatus(resp.StatusCode))
		}
		normalizeOpenAIUsage(&usageResp.Usage)
		applyUsagePostProcessing(info, &usageResp.Usage, nil)
		return &usageResp.Usage, nil
	}

	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		if errors.Is(err, common.ErrReadLimitExceeded) {
			service.MarkUpstreamAccepted(c)
			common.SysError("accepted OpenAI image response exceeded the local response limit; retry suppressed")
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(err)
			}
			return nil, channel.AcceptedResponseDeliveryError()
		}
		service.MarkUpstreamAccepted(c)
		return nil, channel.AcceptedResponseDeliveryError()
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		return nil, channel.AcceptedResponseDeliveryError()
	}

	if oaiError := usageResp.GetOpenAIError(); meaningfulOpenAIError(oaiError) {
		return nil, types.WithOpenAIError(*oaiError, semanticUpstreamErrorStatus(resp.StatusCode))
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

// normalizeOpenAIUsage maps the OpenAI Images usage shape (input_tokens /
// output_tokens / input_tokens_details) onto the canonical prompt/completion
// fields. It is used only on the OpenAI image relay paths (generations/edits,
// streaming and non-streaming): the image API never returns prompt_tokens /
// completion_tokens, so the overwrite (=) semantics here are equivalent to the
// previous additive (+=) behavior while avoiding any future double-counting if
// both field sets are ever populated. Do not reuse this on chat/embedding paths
// without revisiting the overwrite semantics.
func normalizeOpenAIUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return OpenaiImageJSONAsStreamHandler(c, info, resp)
	}
	// Reuse the shared streaming engine (helper.StreamScannerHandler) so the
	// image streaming path gets the same ping keepalive, streaming-timeout
	// watchdog, client-disconnect detection, panic recovery and goroutine
	// cleanup as every other relay stream. The scanner delivers only the
	// "data:" payload, so the SSE "event:" line is rebuilt from the JSON "type"
	// field (real OpenAI image events keep event == type).
	usage := &dto.Usage{}
	var lastStreamData []byte
	var streamErr *types.NewAPIError
	seenValidResponse := false
	seenImageResponse := false
	completed := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		raw := common.StringToByteSlice(data)
		if !json.Valid(raw) {
			service.MarkUpstreamAccepted(c)
			streamErr = channel.AcceptedResponseDeliveryError()
			sr.Stop(fmt.Errorf("invalid JSON in upstream image stream"))
			return
		}
		if isOpenAIImageStreamErrorEvent(raw) {
			sr.Error(errors.New(extractOpenAIImageStreamErrorMessage(raw)))
			if seenValidResponse {
				service.MarkUpstreamAccepted(c)
				streamErr = channel.AcceptedResponseDeliveryError()
			} else {
				streamErr = explicitOpenAIImageStreamRejection(raw, resp.StatusCode)
			}
			sr.Stop(nil)
			return
		}

		var payload struct {
			Type  string    `json:"type"`
			Usage dto.Usage `json:"usage"`
		}
		if err := common.Unmarshal(raw, &payload); err != nil {
			service.MarkUpstreamAccepted(c)
			streamErr = channel.AcceptedResponseDeliveryError()
			sr.Stop(err)
			return
		}
		eventType := strings.ToLower(strings.TrimSpace(payload.Type))
		isImageEvent := strings.HasPrefix(eventType, "image_generation.")
		normalizeOpenAIUsage(&payload.Usage)
		hasUsage := service.ValidUsage(&payload.Usage)
		if !isImageEvent && !hasUsage {
			service.MarkUpstreamAccepted(c)
			streamErr = channel.AcceptedResponseDeliveryError()
			sr.Stop(errors.New("unrecognized upstream image stream event"))
			return
		}

		service.MarkUpstreamAccepted(c)
		seenValidResponse = true
		seenImageResponse = seenImageResponse || isImageEvent
		completed = completed || eventType == "image_generation.completed"
		lastStreamData = raw
		var usageResp dto.SimpleResponse
		if err := common.Unmarshal(raw, &usageResp); err == nil {
			normalizeOpenAIUsage(&usageResp.Usage)
			if service.ValidUsage(&usageResp.Usage) {
				usage = &usageResp.Usage
			}
		}
		if err := writeOpenaiImageStreamChunk(c, raw); err != nil {
			streamErr = channel.AcceptedResponseDeliveryError()
			sr.Stop(err)
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}

	finishedNormally := completed || info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone
	if info.StreamStatus == nil || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() || !seenValidResponse || !seenImageResponse || !finishedNormally {
		service.MarkUpstreamAccepted(c)
		return nil, channel.AcceptedResponseDeliveryError()
	}

	// StreamScannerHandler consumes the upstream [DONE]; re-emit it so the
	// client still receives a terminal data: [DONE].
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone {
		if err := helper.Done(c); err != nil {
			service.MarkUpstreamAccepted(c)
			return nil, channel.AcceptedResponseDeliveryError()
		}
	}

	applyUsagePostProcessing(info, usage, lastStreamData)
	return usage, nil
}

// writeOpenaiImageStreamChunk rebuilds the SSE frame for an image stream chunk:
// it emits an "event:" line derived from the JSON "type" field (when present)
// followed by the verbatim "data:" payload, mirroring helper.ResponseChunkData.
func writeOpenaiImageStreamChunk(c *gin.Context, data []byte) error {
	var payload struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return err
	}
	if eventName := strings.TrimSpace(payload.Type); eventName != "" {
		if !validSSEEventName(eventName) {
			return errors.New("invalid upstream image stream event name")
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
		return err
	}
	return helper.FlushWriter(c)
}

func validSSEEventName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

// isOpenAIImageStreamErrorEvent detects upstream error chunks by JSON content
// only ("type" of error/upstream_error, or a non-empty "error" field). The SSE
// "event:" line is not available here: StreamScannerHandler delivers only the
// "data:" payload. A payload carrying just a "message" key is deliberately NOT
// treated as an error to avoid false positives.
func isOpenAIImageStreamErrorEvent(data []byte) bool {
	if !json.Valid(data) {
		return false
	}
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	return payloadType == "error" || payloadType == "upstream_error" || len(payload.Error) > 0
}

func extractOpenAIImageStreamErrorMessage(data []byte) string {
	if len(data) == 0 || !json.Valid(data) {
		return "upstream image stream returned error event"
	}
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return "upstream image stream returned error event"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg
	}
	if len(payload.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return msg
			}
		}
		if msg := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); msg != "" {
			return msg
		}
	}
	return "upstream image stream returned error event"
}

func explicitOpenAIImageStreamRejection(data []byte, status int) *types.NewAPIError {
	var response dto.SimpleResponse
	if err := common.Unmarshal(data, &response); err == nil {
		if apiErr := explicitOpenAIRejection(response.GetOpenAIError(), status); apiErr != nil {
			return apiErr
		}
	}

	var payload struct {
		Type string `json:"type"`
		Code any    `json:"code"`
	}
	_ = common.Unmarshal(data, &payload)
	return service.MarkExplicitUpstreamRejection(types.WithOpenAIError(types.OpenAIError{
		Type:    payload.Type,
		Message: extractOpenAIImageStreamErrorMessage(data),
		Code:    payload.Code,
	}, semanticUpstreamErrorStatus(status)))
}

func OpenaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	var imageResp struct {
		Data    []dto.ImageData `json:"data"`
		Created int64           `json:"created"`
		Usage   dto.Usage       `json:"usage"`
		Error   any             `json:"error"`
	}
	if err := common.DecodeJsonWithLimit(resp.Body, &imageResp, common.UpstreamJSONBodyLimit()); err != nil {
		service.MarkUpstreamAccepted(c)
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		common.SysError(fmt.Sprintf("accepted OpenAI image JSON stream decode failed: error_type=%T", err))
		return nil, channel.AcceptedResponseDeliveryError()
	}
	usageResp := dto.SimpleResponse{Usage: imageResp.Usage, Error: imageResp.Error}
	if oaiError := usageResp.GetOpenAIError(); meaningfulOpenAIError(oaiError) {
		return nil, types.WithOpenAIError(*oaiError, semanticUpstreamErrorStatus(resp.StatusCode))
	}
	if len(imageResp.Data) == 0 {
		service.MarkUpstreamAccepted(c)
		return nil, channel.AcceptedResponseDeliveryError()
	}
	service.MarkUpstreamAccepted(c)
	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, nil)

	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)

	created := imageResp.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	if info != nil {
		info.SetFirstResponseTime()
	}
	for _, image := range imageResp.Data {
		if err := writeOpenaiImageStreamCompleted(c, created, image, &usageResp.Usage); err != nil {
			if info != nil {
				if info.StreamStatus == nil {
					info.StreamStatus = relaycommon.NewStreamStatus()
				}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			}
			return &usageResp.Usage, channel.AcceptedResponseDeliveryError()
		}
	}
	if err := writeOpenaiImageStreamDone(c); err != nil {
		if info != nil {
			if info.StreamStatus == nil {
				info.StreamStatus = relaycommon.NewStreamStatus()
			}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		}
		return &usageResp.Usage, channel.AcceptedResponseDeliveryError()
	}
	if info != nil {
		info.ReceivedResponseCount += len(imageResp.Data)
		if info.StreamStatus == nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	}
	return &usageResp.Usage, nil
}

func writeOpenaiImageStreamCompleted(c *gin.Context, created int64, image dto.ImageData, usage *dto.Usage) error {
	if _, err := fmt.Fprintf(c.Writer, "event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"created_at\":%d", created); err != nil {
		return err
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "url", value: image.Url},
		{name: "b64_json", value: image.B64Json},
		{name: "revised_prompt", value: image.RevisedPrompt},
	}
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		if _, err := fmt.Fprintf(c.Writer, ",\"%s\":", field.name); err != nil {
			return err
		}
		if err := common.WriteJSONString(c.Writer, field.value); err != nil {
			return err
		}
	}
	if service.ValidUsage(usage) {
		usageJSON, err := common.Marshal(usage)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, ",\"usage\":%s", usageJSON); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(c.Writer, "}\n\n"); err != nil {
		return err
	}
	return helper.FlushWriter(c)
}

func writeOpenaiImageStreamDone(c *gin.Context) error {
	if _, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n"); err != nil {
		return err
	}
	return helper.FlushWriter(c)
}
