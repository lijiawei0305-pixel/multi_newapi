package coze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func parseCozeCreateResponse(c *gin.Context, responseBody []byte) (*CozeChatResponse, error) {
	var response CozeChatResponse
	if err := basecommon.Unmarshal(responseBody, &response); err != nil {
		service.MarkUpstreamAccepted(c)
		return nil, err
	}
	if response.Code != 0 {
		return nil, service.ExplicitUpstreamRejection(fmt.Errorf("Coze rejected request: code=%d message=%s", response.Code, response.Msg))
	}
	if strings.TrimSpace(response.Data.Id) == "" || strings.TrimSpace(response.Data.ConversationId) == "" {
		service.MarkUpstreamAccepted(c)
		return nil, errors.New("Coze accepted response omitted chat identifiers")
	}
	service.MarkUpstreamAccepted(c)
	return &response, nil
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *common.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

// ConvertAudioRequest implements channel.Adaptor.
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *common.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

// ConvertClaudeRequest implements channel.Adaptor.
func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *common.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, channel.NewUnsupportedConversionError("Coze", "Claude")
}

// ConvertEmbeddingRequest implements channel.Adaptor.
func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *common.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertImageRequest implements channel.Adaptor.
func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *common.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertOpenAIRequest implements channel.Adaptor.
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *common.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return convertCozeChatRequest(c, *request)
}

// ConvertOpenAIResponsesRequest implements channel.Adaptor.
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *common.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertRerankRequest implements channel.Adaptor.
func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// DoRequest implements channel.Adaptor.
func (a *Adaptor) DoRequest(c *gin.Context, info *common.RelayInfo, requestBody io.Reader) (any, error) {
	if info.IsStream {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
	// 首先发送创建消息请求，成功后再发送获取消息请求
	// 发送创建消息请求
	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, nil
	}
	defer service.CloseResponseBodyGracefully(resp)
	respBody, err := basecommon.ReadAllWithLimit(resp.Body)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		return nil, err
	}
	cozeResponse, err := parseCozeCreateResponse(c, respBody)
	if err != nil {
		return nil, err
	}
	c.Set("coze_conversation_id", cozeResponse.Data.ConversationId)
	c.Set("coze_chat_id", cozeResponse.Data.Id)

	pollTimeoutSeconds := basecommon.GetEnvOrDefault("RELAY_COZE_POLL_TIMEOUT_SECONDS", 210)
	if pollTimeoutSeconds <= 0 || pollTimeoutSeconds > 3600 {
		pollTimeoutSeconds = 210
	}
	pollCtx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(pollTimeoutSeconds)*time.Second)
	defer cancel()
	if err := waitForCozeChat(pollCtx, time.Second, func(ctx context.Context) (bool, error) {
		return checkIfChatComplete(a, c, info, ctx)
	}); err != nil {
		// Creation already returned stable identifiers, so the provider accepted
		// the work. A timeout, cancellation, or later polling failure must never
		// retry the request or refund its immutable reservation.
		service.MarkUpstreamAccepted(c)
		return nil, channel.AcceptedResponseDeliveryError()
	}
	cancel()
	// 发送获取消息请求
	return getChatDetail(a, c, info, c.Request.Context())
}

func waitForCozeChat(ctx context.Context, interval time.Duration, check func(context.Context) (bool, error)) error {
	if ctx == nil {
		return errors.New("missing Coze polling context")
	}
	if check == nil {
		return errors.New("missing Coze polling check")
	}
	if interval <= 0 {
		return errors.New("invalid Coze polling interval")
	}
	for {
		complete, err := check(ctx)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// DoResponse implements channel.Adaptor.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *common.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.IsStream {
		usage, err = cozeChatStreamHandler(c, info, resp)
	} else {
		usage, err = cozeChatHandler(c, info, resp)
	}
	return
}

// GetChannelName implements channel.Adaptor.
func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// GetModelList implements channel.Adaptor.
func (a *Adaptor) GetModelList() []string {
	return ModelList
}

// GetRequestURL implements channel.Adaptor.
func (a *Adaptor) GetRequestURL(info *common.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v3/chat", info.ChannelBaseUrl), nil
}

// Init implements channel.Adaptor.
func (a *Adaptor) Init(info *common.RelayInfo) {

}

// SetupRequestHeader implements channel.Adaptor.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *common.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}
