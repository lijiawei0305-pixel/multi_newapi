package channel

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/types"
)

// AcceptedResponseDeliveryError is safe to expose after a successful provider
// status when the local response cannot be parsed or delivered. The accepted
// marker prevents retry/refund; callers with unknown usage let the controller
// conservatively finalize the immutable reserved quota.
func AcceptedResponseDeliveryError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("upstream accepted the request but its response could not be delivered safely; do not retry"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
}
