package minimax

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type MiniMaxTTSRequest struct {
	Model             string             `json:"model"`
	Text              string             `json:"text"`
	Stream            *bool              `json:"stream,omitempty"`
	StreamOptions     *StreamOptions     `json:"stream_options,omitempty"`
	VoiceSetting      VoiceSetting       `json:"voice_setting"`
	PronunciationDict *PronunciationDict `json:"pronunciation_dict,omitempty"`
	AudioSetting      *AudioSetting      `json:"audio_setting,omitempty"`
	TimbreWeights     []TimbreWeight     `json:"timbre_weights,omitempty"`
	LanguageBoost     string             `json:"language_boost,omitempty"`
	VoiceModify       *VoiceModify       `json:"voice_modify,omitempty"`
	SubtitleEnable    *bool              `json:"subtitle_enable,omitempty"`
	OutputFormat      string             `json:"output_format,omitempty"`
	AigcWatermark     *bool              `json:"aigc_watermark,omitempty"`
}

type StreamOptions struct {
	ExcludeAggregatedAudio *bool `json:"exclude_aggregated_audio,omitempty"`
}

type VoiceSetting struct {
	VoiceID           string   `json:"voice_id"`
	Speed             *float64 `json:"speed,omitempty"`
	Vol               *float64 `json:"vol,omitempty"`
	Pitch             *int     `json:"pitch,omitempty"`
	Emotion           string   `json:"emotion,omitempty"`
	TextNormalization *bool    `json:"text_normalization,omitempty"`
	LatexRead         *bool    `json:"latex_read,omitempty"`
}

type PronunciationDict struct {
	Tone []string `json:"tone,omitempty"`
}

type AudioSetting struct {
	SampleRate *int   `json:"sample_rate,omitempty"`
	Bitrate    *int   `json:"bitrate,omitempty"`
	Format     string `json:"format,omitempty"`
	Channel    *int   `json:"channel,omitempty"`
	ForceCbr   *bool  `json:"force_cbr,omitempty"`
}

type TimbreWeight struct {
	VoiceID string `json:"voice_id"`
	Weight  int    `json:"weight"`
}

type VoiceModify struct {
	Pitch        *int   `json:"pitch,omitempty"`
	Intensity    *int   `json:"intensity,omitempty"`
	Timbre       *int   `json:"timbre,omitempty"`
	SoundEffects string `json:"sound_effects,omitempty"`
}

type MiniMaxTTSResponse struct {
	Data      MiniMaxTTSData   `json:"data"`
	ExtraInfo MiniMaxExtraInfo `json:"extra_info"`
	TraceID   string           `json:"trace_id"`
	BaseResp  MiniMaxBaseResp  `json:"base_resp"`
}

type MiniMaxTTSData struct {
	Audio  string `json:"audio"`
	Status int    `json:"status"`
}

type MiniMaxExtraInfo struct {
	UsageCharacters int64 `json:"usage_characters"`
}

type MiniMaxBaseResp struct {
	StatusCode int64  `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

func getContentTypeByFormat(format string) string {
	contentTypeMap := map[string]string{
		"mp3":  "audio/mpeg",
		"wav":  "audio/wav",
		"flac": "audio/flac",
		"aac":  "audio/aac",
		"pcm":  "audio/pcm",
	}
	if ct, ok := contentTypeMap[format]; ok {
		return ct
	}
	return "audio/mpeg" // default to mp3
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	defer resp.Body.Close()
	var minimaxResp MiniMaxTTSResponse
	decodeErr := common.DecodeJsonWithLimit(resp.Body, &minimaxResp, common.UpstreamJSONBodyLimit())
	if decodeErr != nil {
		service.MarkUpstreamAccepted(c)
		if errors.Is(decodeErr, common.ErrReadLimitExceeded) {
			common.SysError("accepted minimax TTS response exceeded the local response limit; retry suppressed")
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(decodeErr)
			}
			return nil, channel.AcceptedResponseDeliveryError()
		}
		common.SysError(fmt.Sprintf("accepted minimax TTS JSON decode failed: error_type=%T", decodeErr))
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(decodeErr)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}

	// Check base_resp status code
	if minimaxResp.BaseResp.StatusCode != 0 {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("minimax TTS error: %d - %s", minimaxResp.BaseResp.StatusCode, minimaxResp.BaseResp.StatusMsg),
			types.ErrorCodeBadResponse,
			http.StatusBadRequest,
		)
	}
	service.MarkUpstreamAccepted(c)

	// Check if we have audio data
	if minimaxResp.Data.Audio == "" {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("no audio data in minimax TTS response"),
			types.ErrorCodeBadResponse,
			http.StatusBadRequest,
		)
	}

	usage = &dto.Usage{
		PromptTokens:     info.GetEstimatePromptTokens(),
		CompletionTokens: 0,
		TotalTokens:      int(minimaxResp.ExtraInfo.UsageCharacters),
	}

	if strings.HasPrefix(minimaxResp.Data.Audio, "http") {
		c.Redirect(http.StatusFound, minimaxResp.Data.Audio)
	} else {
		// Determine content type - default to mp3
		contentType := "audio/mpeg"
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
		decoder := hex.NewDecoder(strings.NewReader(minimaxResp.Data.Audio))
		if _, decodeErr := io.Copy(c.Writer, decoder); decodeErr != nil {
			// The provider has already returned success; never turn a local spool
			// or decode failure into a duplicate upstream synthesis attempt.
			common.SysError("failed to spool accepted minimax TTS audio: " + decodeErr.Error())
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(decodeErr)
			}
		}
	}

	return usage, nil
}

func handleChatCompletionResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("minimax response is unavailable"),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, readErr := common.ReadAllWithLimit(resp.Body)
	if readErr != nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("failed to read minimax response"),
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusInternalServerError,
		)
	}
	service.CopyUpstreamResponseHeaders(c, c.Writer.Header(), resp.Header)

	c.Data(resp.StatusCode, "application/json", body)
	return nil, nil
}
