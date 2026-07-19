package service

import (
	"errors"

	"github.com/QuantumNous/new-api/types"
)

// explicitUpstreamRejection marks a parsed provider response that
// affirmatively says synchronous work was rejected. Transport, decoding, and
// local conversion errors must not use this marker because the provider may
// already have accepted the request.
type explicitUpstreamRejection struct {
	err error
}

func (e *explicitUpstreamRejection) Error() string {
	return e.err.Error()
}

func (e *explicitUpstreamRejection) Unwrap() error {
	return e.err
}

func ExplicitUpstreamRejection(err error) error {
	if err == nil {
		return nil
	}
	return &explicitUpstreamRejection{err: err}
}

func MarkExplicitUpstreamRejection(apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil || IsExplicitUpstreamRejection(apiErr) {
		return apiErr
	}
	if apiErr.Err == nil {
		apiErr.Err = errors.New("upstream rejected request")
	}
	apiErr.Err = ExplicitUpstreamRejection(apiErr.Err)
	return apiErr
}

func IsExplicitUpstreamRejection(err error) bool {
	var rejection *explicitUpstreamRejection
	return errors.As(err, &rejection)
}
