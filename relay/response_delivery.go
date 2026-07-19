package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func optionalHTTPResponse(value any) (*http.Response, *types.NewAPIError) {
	if value == nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("upstream returned no HTTP response"),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	response, ok := value.(*http.Response)
	if ok && response != nil {
		return response, nil
	}
	return nil, types.NewErrorWithStatusCode(
		errors.New("upstream returned an invalid response"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)
}

func allowsNilHTTPResponse(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}

	switch info.ChannelType {
	case constant.ChannelTypeAws:
		if info.ChannelOtherSettings.AwsKeyType == dto.AwsKeyTypeApiKey {
			return false
		}
		switch info.RelayFormat {
		case types.RelayFormatOpenAI:
			return info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayMode == relayconstant.RelayModeCompletions
		case types.RelayFormatClaude:
			return info.RelayMode == relayconstant.RelayModeUnknown || info.RelayMode == relayconstant.RelayModeChatCompletions
		}
	case constant.ChannelTypeXunfei:
		return info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions
	}

	return false
}

func resolveSynchronousHTTPResponse(value any, info *relaycommon.RelayInfo) (*http.Response, *types.NewAPIError) {
	if value == nil && allowsNilHTTPResponse(info) {
		return nil, nil
	}
	return optionalHTTPResponse(value)
}

func acceptedResponseUsage(c *gin.Context, value any) (*dto.Usage, *types.NewAPIError) {
	usage, ok := value.(*dto.Usage)
	if ok && usage != nil {
		return usage, nil
	}
	err := errors.New("accepted response usage is unavailable")
	if c != nil {
		if buffered, bufferedOK := c.Writer.(*common.BufferedResponseWriter); bufferedOK {
			_ = buffered.Fail(err)
		}
	}
	common.SysError(fmt.Sprintf("accepted response returned invalid usage: usage_type=%T", value))
	return nil, channel.AcceptedResponseDeliveryError()
}

func recordSynchronousUpstreamResult(c *gin.Context, response *http.Response, apiErr *types.NewAPIError) {
	if apiErr == nil {
		if response == nil || (response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices) {
			service.MarkUpstreamAccepted(c)
		}
		return
	}
	if response != nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices && !service.IsExplicitUpstreamRejection(apiErr) {
		service.MarkUpstreamAccepted(c)
	}
}

func requiredWebSocketConnection(value any) (*websocket.Conn, *types.NewAPIError) {
	connection, ok := value.(*websocket.Conn)
	if ok && connection != nil {
		return connection, nil
	}
	return nil, types.NewErrorWithStatusCode(
		errors.New("upstream websocket connection is unavailable"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)
}

func acceptedRealtimeUsage(value any) (*dto.RealtimeUsage, *types.NewAPIError) {
	usage, ok := value.(*dto.RealtimeUsage)
	if ok && usage != nil {
		return usage, nil
	}
	common.SysError(fmt.Sprintf("accepted realtime response returned invalid usage: usage_type=%T", value))
	return nil, channel.AcceptedResponseDeliveryError()
}

// acceptedResponseDeliveryError converts a private spool failure into a safe,
// terminal client error. Callers invoke it only after durable usage settlement
// so an accepted provider response is neither retried nor refunded.
func acceptedResponseDeliveryError(c *gin.Context) *types.NewAPIError {
	if c == nil || !service.IsUpstreamAccepted(c) {
		return nil
	}
	buffered, ok := c.Writer.(*common.BufferedResponseWriter)
	if !ok || buffered.Err() == nil {
		return nil
	}
	common.SysError(fmt.Sprintf("accepted response delivery failed after durable billing: error_type=%T", buffered.Err()))
	return channel.AcceptedResponseDeliveryError()
}
