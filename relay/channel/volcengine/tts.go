package volcengine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type VolcengineTTSRequest struct {
	App     VolcengineTTSApp     `json:"app"`
	User    VolcengineTTSUser    `json:"user"`
	Audio   VolcengineTTSAudio   `json:"audio"`
	Request VolcengineTTSReqInfo `json:"request"`
}

type VolcengineTTSApp struct {
	AppID   string `json:"appid"`
	Token   string `json:"token"`
	Cluster string `json:"cluster"`
}

type VolcengineTTSUser struct {
	UID string `json:"uid"`
}

type VolcengineTTSAudio struct {
	VoiceType        string   `json:"voice_type"`
	Encoding         string   `json:"encoding"`
	SpeedRatio       *float64 `json:"speed_ratio,omitempty"`
	Rate             int      `json:"rate"`
	Bitrate          *int     `json:"bitrate,omitempty"`
	LoudnessRatio    *float64 `json:"loudness_ratio,omitempty"`
	EnableEmotion    *bool    `json:"enable_emotion,omitempty"`
	Emotion          string   `json:"emotion,omitempty"`
	EmotionScale     *float64 `json:"emotion_scale,omitempty"`
	ExplicitLanguage string   `json:"explicit_language,omitempty"`
	ContextLanguage  string   `json:"context_language,omitempty"`
}

type VolcengineTTSReqInfo struct {
	ReqID           string                   `json:"reqid"`
	Text            string                   `json:"text"`
	Operation       string                   `json:"operation"`
	Model           string                   `json:"model,omitempty"`
	TextType        string                   `json:"text_type,omitempty"`
	SilenceDuration *float64                 `json:"silence_duration,omitempty"`
	WithTimestamp   interface{}              `json:"with_timestamp,omitempty"`
	ExtraParam      *VolcengineTTSExtraParam `json:"extra_param,omitempty"`
}

type VolcengineTTSExtraParam struct {
	DisableMarkdownFilter      *bool                     `json:"disable_markdown_filter,omitempty"`
	EnableLatexTn              *bool                     `json:"enable_latex_tn,omitempty"`
	MuteCutThreshold           string                    `json:"mute_cut_threshold,omitempty"`
	MuteCutRemainMs            string                    `json:"mute_cut_remain_ms,omitempty"`
	DisableEmojiFilter         *bool                     `json:"disable_emoji_filter,omitempty"`
	UnsupportedCharRatioThresh *float64                  `json:"unsupported_char_ratio_thresh,omitempty"`
	AigcWatermark              *bool                     `json:"aigc_watermark,omitempty"`
	CacheConfig                *VolcengineTTSCacheConfig `json:"cache_config,omitempty"`
}

type VolcengineTTSCacheConfig struct {
	TextType *int  `json:"text_type,omitempty"`
	UseCache *bool `json:"use_cache,omitempty"`
}

type VolcengineTTSResponse struct {
	ReqID    string                     `json:"reqid"`
	Code     int                        `json:"code"`
	Message  string                     `json:"message"`
	Sequence int                        `json:"sequence"`
	Data     string                     `json:"data"`
	Addition *VolcengineTTSAdditionInfo `json:"addition,omitempty"`
}

type VolcengineTTSAdditionInfo struct {
	Duration string `json:"duration"`
}

var openAIToVolcengineVoiceMap = map[string]string{
	"alloy":   "zh_male_M392_conversation_wvae_bigtts",
	"echo":    "zh_male_wenhao_mars_bigtts",
	"fable":   "zh_female_tianmei_mars_bigtts",
	"onyx":    "zh_male_zhibei_mars_bigtts",
	"nova":    "zh_female_shuangkuaisisi_mars_bigtts",
	"shimmer": "zh_female_cancan_mars_bigtts",
}

var responseFormatToEncodingMap = map[string]string{
	"mp3":  "mp3",
	"opus": "ogg_opus",
	"aac":  "mp3",
	"flac": "mp3",
	"wav":  "wav",
	"pcm":  "pcm",
}

// UsesNativeTTSWebSocket identifies the native Volc speech endpoint. Custom
// bases use their HTTP response exactly once and must never be switched into a
// second WebSocket request merely because the client requested streaming.
func UsesNativeTTSWebSocket(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != channelconstant.ChannelTypeVolcEngine || info.RelayMode != relayconstant.RelayModeAudioSpeech {
		return false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	defaultBaseURL := strings.TrimRight(channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine], "/")
	return baseURL == "" || baseURL == defaultBaseURL
}

func parseVolcengineAuth(apiKey string) (appID, token string, err error) {
	parts := strings.Split(apiKey, "|")
	if len(parts) != 2 {
		return "", "", errors.New("invalid api key format, expected: appid|access_token")
	}
	return parts[0], parts[1], nil
}

func mapVoiceType(openAIVoice string) string {
	if voice, ok := openAIToVolcengineVoiceMap[openAIVoice]; ok {
		return voice
	}
	return openAIVoice
}

func mapEncoding(responseFormat string) string {
	if encoding, ok := responseFormatToEncodingMap[responseFormat]; ok {
		return encoding
	}
	return "mp3"
}

func getContentTypeByEncoding(encoding string) string {
	contentTypeMap := map[string]string{
		"mp3":      "audio/mpeg",
		"ogg_opus": "audio/ogg",
		"wav":      "audio/wav",
		"pcm":      "audio/pcm",
	}
	if ct, ok := contentTypeMap[encoding]; ok {
		return ct
	}
	return "application/octet-stream"
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, encoding string) (usage any, err *types.NewAPIError) {
	defer resp.Body.Close()
	var volcResp VolcengineTTSResponse
	decodeErr := common.DecodeJsonWithLimit(resp.Body, &volcResp, common.UpstreamJSONBodyLimit())
	if decodeErr != nil {
		service.MarkUpstreamAccepted(c)
		if errors.Is(decodeErr, common.ErrReadLimitExceeded) {
			common.SysError("accepted volcengine TTS response exceeded the local response limit; retry suppressed")
			if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
				_ = buffered.Fail(decodeErr)
			}
			return nil, channel.AcceptedResponseDeliveryError()
		}
		common.SysError(fmt.Sprintf("accepted volcengine TTS JSON decode failed: error_type=%T", decodeErr))
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(decodeErr)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}

	if volcResp.Code != 3000 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(volcResp.Message),
			types.ErrorCodeBadResponse,
			http.StatusBadRequest,
		)
	}
	service.MarkUpstreamAccepted(c)
	usage = &dto.Usage{
		PromptTokens:     info.GetEstimatePromptTokens(),
		CompletionTokens: 0,
		TotalTokens:      info.GetEstimatePromptTokens(),
	}

	contentType := getContentTypeByEncoding(encoding)
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(volcResp.Data))
	if _, decodeErr := io.Copy(c.Writer, decoder); decodeErr != nil {
		// A 2xx provider response is already accepted. Returning a retryable
		// adaptor error here could generate/bill the same audio twice.
		common.SysError("failed to spool accepted volcengine TTS audio: " + decodeErr.Error())
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(decodeErr)
		}
	}

	return usage, nil
}

func generateRequestID() string {
	return uuid.New().String()
}

func handleTTSWebSocketResponse(c *gin.Context, requestURL string, volcRequest VolcengineTTSRequest, info *relaycommon.RelayInfo, encoding string) (usage any, err *types.NewAPIError) {
	_, token, parseErr := parseVolcengineAuth(info.ApiKey)
	if parseErr != nil {
		return nil, types.NewErrorWithStatusCode(
			parseErr,
			types.ErrorCodeChannelInvalidKey,
			http.StatusUnauthorized,
		)
	}

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer;%s", token))

	requestContext := context.Background()
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	conn, resp, dialErr := websocket.DefaultDialer.DialContext(requestContext, requestURL, header)
	if dialErr != nil {
		if resp != nil {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("failed to connect to websocket: %w, status: %d", dialErr, resp.StatusCode),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusBadGateway,
			)
		}
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("failed to connect to websocket: %w", dialErr),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusBadGateway,
		)
	}
	defer conn.Close()
	refreshReadDeadline := prepareVolcengineTTSWebSocket(conn)
	stopCancelWatch := closeVolcengineTTSWebSocketOnCancel(requestContext, conn)
	defer stopCancelWatch()

	payload, marshalErr := common.Marshal(volcRequest)
	if marshalErr != nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("failed to marshal request: %w", marshalErr),
			types.ErrorCodeBadRequestBody,
			http.StatusInternalServerError,
		)
	}

	if sendErr := FullClientRequest(conn, payload); sendErr != nil {
		return nil, acceptedVolcengineWebSocketFailure(c, sendErr)
	}

	contentType := getContentTypeByEncoding(encoding)
	c.Header("Content-Type", contentType)
	c.Header("Transfer-Encoding", "chunked")
	receivedAudio := false

	for {
		if deadlineErr := refreshReadDeadline(); deadlineErr != nil {
			return nil, acceptedVolcengineWebSocketFailure(c, deadlineErr)
		}
		msg, recvErr := ReceiveMessage(conn)
		if recvErr != nil {
			if errors.Is(recvErr, websocket.ErrReadLimit) {
				service.MarkUpstreamAccepted(c)
				common.SysError("accepted volcengine websocket frame exceeded the local message limit")
				return nil, channel.AcceptedResponseDeliveryError()
			}
			if websocket.IsCloseError(recvErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil, acceptedVolcengineWebSocketFailure(c, recvErr)
			}
			return nil, acceptedVolcengineWebSocketFailure(c, recvErr)
		}

		switch msg.MsgType {
		case MsgTypeError:
			if service.IsUpstreamAccepted(c) {
				return nil, acceptedVolcengineWebSocketFailure(c, errors.New("provider reported an error after accepted audio"))
			}
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("received error from server: code=%d, %s", msg.ErrorCode, string(msg.Payload)),
				types.ErrorCodeBadResponse,
				http.StatusBadRequest,
			)
		case MsgTypeFrontEndResultServer:
			continue
		case MsgTypeAudioOnlyServer:
			if len(msg.Payload) > 0 {
				if writeErr := writeAcceptedVolcengineAudio(c, msg.Payload); writeErr != nil {
					return nil, acceptedVolcengineWebSocketFailure(c, writeErr)
				}
				receivedAudio = true
			}

			if msg.Sequence < 0 {
				if !receivedAudio {
					return nil, acceptedVolcengineWebSocketFailure(c, errors.New("provider completed without audio"))
				}
				c.Status(http.StatusOK)
				usage = &dto.Usage{
					PromptTokens:     info.GetEstimatePromptTokens(),
					CompletionTokens: 0,
					TotalTokens:      info.GetEstimatePromptTokens(),
				}
				return usage, nil
			}
		default:
			continue
		}
	}
}

func prepareVolcengineTTSWebSocket(conn *websocket.Conn) func() error {
	frameLimit := int64(common.GetEnvOrDefault("RELAY_VOLCENGINE_TTS_WS_MAX_FRAME_BYTES", 4<<20))
	if frameLimit <= 0 {
		frameLimit = 4 << 20
	}
	if responseLimit := common.BufferedResponseBodyLimit(); frameLimit > responseLimit {
		frameLimit = responseLimit
	}
	conn.SetReadLimit(frameLimit)
	idleSeconds := common.GetEnvOrDefault("RELAY_VOLCENGINE_TTS_WS_IDLE_SECONDS", 30)
	if idleSeconds <= 0 {
		idleSeconds = 30
	}
	totalSeconds := common.GetEnvOrDefault("RELAY_VOLCENGINE_TTS_WS_TOTAL_SECONDS", 300)
	if totalSeconds <= 0 {
		totalSeconds = 300
	}
	totalDeadline := time.Now().Add(time.Duration(totalSeconds) * time.Second)
	refresh := func() error {
		deadline := time.Now().Add(time.Duration(idleSeconds) * time.Second)
		if deadline.After(totalDeadline) {
			deadline = totalDeadline
		}
		return conn.SetReadDeadline(deadline)
	}
	conn.SetPongHandler(func(string) error { return refresh() })
	return refresh
}

func closeVolcengineTTSWebSocketOnCancel(ctx context.Context, conn *websocket.Conn) func() {
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopped:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stopped) }) }
}

func writeAcceptedVolcengineAudio(c *gin.Context, payload []byte) error {
	if c == nil || len(payload) == 0 {
		return nil
	}
	service.MarkUpstreamAccepted(c)
	if _, err := c.Writer.Write(payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func acceptedVolcengineWebSocketFailure(c *gin.Context, err error) *types.NewAPIError {
	service.MarkUpstreamAccepted(c)
	common.SysError(fmt.Sprintf("volcengine websocket state became unknown after send: error_type=%T", err))
	return channel.AcceptedResponseDeliveryError()
}
