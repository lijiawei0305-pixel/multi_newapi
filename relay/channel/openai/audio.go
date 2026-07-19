package openai

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func OpenaiTTSHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	// the status code has been judged before, if there is a body reading failure,
	// it should be regarded as a non-recoverable error, so it should not return err for external retry.
	// Analogous to nginx's load balancing, it will only retry if it can't be requested or
	// if the upstream returns a specific status code, once the upstream has already written the header,
	// the subsequent failure of the response body should be regarded as a non-recoverable error,
	// and can be terminated directly.
	defer service.CloseResponseBodyGracefully(resp)
	usage := &dto.Usage{}
	usage.PromptTokens = info.GetEstimatePromptTokens()
	usage.TotalTokens = info.GetEstimatePromptTokens()
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		responseBody, readErr := common.ReadAllWithLimit(resp.Body)
		if readErr != nil {
			service.MarkUpstreamAccepted(c)
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(readErr)
			}
			common.SysError(fmt.Sprintf("accepted OpenAI TTS JSON read failed: error_type=%T", readErr))
			return nil, channel.AcceptedResponseDeliveryError()
		}
		var errorResponse dto.SimpleResponse
		if decodeErr := common.Unmarshal(responseBody, &errorResponse); decodeErr != nil {
			service.MarkUpstreamAccepted(c)
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(decodeErr)
			}
			common.SysError(fmt.Sprintf("accepted OpenAI TTS JSON decode failed: error_type=%T", decodeErr))
			return nil, channel.AcceptedResponseDeliveryError()
		}
		if oaiError := errorResponse.GetOpenAIError(); meaningfulOpenAIError(oaiError) {
			return nil, types.WithOpenAIError(*oaiError, http.StatusBadGateway)
		}
		service.MarkUpstreamAccepted(c)
		err := errors.New("accepted OpenAI TTS response was JSON without audio")
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}
	service.MarkUpstreamAccepted(c)
	service.CopyUpstreamResponseHeaders(c, c.Writer.Header(), resp.Header)
	c.Writer.WriteHeader(resp.StatusCode)

	if info.IsStream {
		helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
			if service.SundaySearch(data, "usage") {
				var simpleResponse dto.SimpleResponse
				if err := common.Unmarshal([]byte(data), &simpleResponse); err != nil {
					logger.LogError(c, err.Error())
					sr.Error(err)
				} else if simpleResponse.Usage.TotalTokens != 0 {
					usage.PromptTokens = simpleResponse.Usage.InputTokens
					usage.CompletionTokens = simpleResponse.OutputTokens
					usage.TotalTokens = simpleResponse.TotalTokens
				}
			}
			if err := helper.StringData(c, data); err != nil {
				sr.Error(err)
			}
		})
	} else {
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		// Stream directly into the reserved hybrid response spool. This avoids a
		// second full-size []byte for long audio while preserving the accepted
		// response even when duration inspection later fails.
		c.Writer.WriteHeaderNow()
		written, copyErr := io.Copy(c.Writer, resp.Body)
		if copyErr != nil {
			logger.LogError(c, fmt.Sprintf("failed to spool accepted TTS response: %v", copyErr))
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(copyErr)
			}
			return nil, channel.AcceptedResponseDeliveryError()
		}

		// 计算音频时长并更新 usage
		audioFormat := "mp3" // 默认格式
		if audioReq, ok := info.Request.(*dto.AudioRequest); ok && audioReq.ResponseFormat != "" {
			audioFormat = audioReq.ResponseFormat
		}

		var duration float64
		var durationErr error

		if copyErr != nil {
			durationErr = copyErr
		} else if audioFormat == "pcm" {
			// PCM 格式没有文件头，根据 OpenAI TTS 的 PCM 参数计算时长
			// 采样率: 24000 Hz, 位深度: 16-bit (2 bytes), 声道数: 1
			const sampleRate = 24000
			const bytesPerSample = 2
			const channels = 1
			duration = float64(written) / float64(sampleRate*bytesPerSample*channels)
		} else {
			ext := "." + audioFormat
			buffered, ok := c.Writer.(*common.BufferedResponseWriter)
			if !ok {
				durationErr = fmt.Errorf("buffered TTS response inspection is unavailable")
			} else {
				durationErr = buffered.InspectBody(func(reader io.ReadSeeker) error {
					var err error
					duration, err = common.GetAudioDuration(c.Request.Context(), reader, ext)
					return err
				})
			}
		}

		usage.PromptTokensDetails.TextTokens = usage.PromptTokens

		if durationErr != nil {
			logger.LogWarn(c, fmt.Sprintf("failed to get audio duration: %v", durationErr))
			// 如果无法获取时长，则设置保底的 CompletionTokens，根据body大小计算
			sizeInKB := float64(written) / 1000.0
			estimatedTokens := int(math.Ceil(sizeInKB)) // 粗略估算每KB约等于1 token
			usage.CompletionTokens = estimatedTokens
			usage.CompletionTokenDetails.AudioTokens = estimatedTokens
		} else if duration > 0 {
			// 计算 token: ceil(duration) / 60.0 * 1000，即每分钟 1000 tokens
			completionTokens := int(math.Round(math.Ceil(duration) / 60.0 * 1000))
			usage.CompletionTokens = completionTokens
			usage.CompletionTokenDetails.AudioTokens = completionTokens
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return usage, nil
}

func OpenaiSTTHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (*types.NewAPIError, *dto.Usage) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		common.SysError(fmt.Sprintf("accepted OpenAI transcription response read failed: error_type=%T", err))
		return channel.AcceptedResponseDeliveryError(), nil
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "json") || responseFormat == "json" || responseFormat == "verbose_json" {
		var errorResponse dto.SimpleResponse
		if decodeErr := common.Unmarshal(responseBody, &errorResponse); decodeErr != nil {
			service.MarkUpstreamAccepted(c)
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(decodeErr)
			}
			common.SysError(fmt.Sprintf("accepted OpenAI transcription JSON decode failed: error_type=%T", decodeErr))
			return channel.AcceptedResponseDeliveryError(), nil
		}
		if oaiError := errorResponse.GetOpenAIError(); meaningfulOpenAIError(oaiError) {
			return types.WithOpenAIError(*oaiError, http.StatusBadGateway), nil
		}
	}
	service.MarkUpstreamAccepted(c)
	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	var responseData struct {
		Usage *dto.Usage `json:"usage"`
	}
	if err := common.Unmarshal(responseBody, &responseData); err == nil && responseData.Usage != nil {
		if responseData.Usage.TotalTokens > 0 {
			usage := responseData.Usage
			if usage.PromptTokens == 0 {
				usage.PromptTokens = usage.InputTokens
			}
			if usage.CompletionTokens == 0 {
				usage.CompletionTokens = usage.OutputTokens
			}
			return nil, usage
		}
	}

	usage := &dto.Usage{}
	usage.PromptTokens = info.GetEstimatePromptTokens()
	usage.CompletionTokens = 0
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return nil, usage
}
