package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
)

// ValidateMediaStreamingCapability keeps client-declared streaming from
// disabling response admission and total upstream deadlines on media adaptors
// that only return an ordinary JSON/binary response.
func ValidateMediaStreamingCapability(info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || !info.IsStream {
		return nil
	}
	if info.ChannelMeta == nil {
		return types.NewErrorWithStatusCode(
			errors.New("streaming media channel capability is unavailable"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	supported := false
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		// The native OpenAI image adaptor implements both JSON-as-SSE and real
		// event-stream delivery. Other image adaptors in this relay are
		// synchronous even if a client supplies stream=true.
		supported = info.ChannelType == constant.ChannelTypeOpenAI
	case relayconstant.RelayModeAudioSpeech:
		supported = info.ChannelType == constant.ChannelTypeOpenAI || volcengine.UsesNativeTTSWebSocket(info)
	case relayconstant.RelayModeAudioTranslation, relayconstant.RelayModeAudioTranscription:
		// The current transcription/translation handlers consume one bounded
		// response body; they do not implement an SSE response contract.
		supported = false
	default:
		return nil
	}

	if supported {
		return nil
	}
	return types.NewErrorWithStatusCode(
		errors.New("streaming is not supported by the selected media channel"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
