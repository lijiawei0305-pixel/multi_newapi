package relay

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
)

func TestTaskHTTPResponseExplicitRejectionWhitelist(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "validation", status: http.StatusBadRequest, want: true},
		{name: "auth", status: http.StatusUnauthorized, want: true},
		{name: "forbidden", status: http.StatusForbidden, want: true},
		{name: "not found", status: http.StatusNotFound, want: true},
		{name: "payload too large", status: http.StatusRequestEntityTooLarge, want: true},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, want: true},
		{name: "rate limited", status: http.StatusTooManyRequests, want: true},
		{name: "timeout remains uncertain", status: http.StatusRequestTimeout, want: false},
		{name: "conflict remains uncertain", status: http.StatusConflict, want: false},
		{name: "unknown 4xx remains uncertain", status: 418, want: false},
		{name: "client reset remains uncertain", status: 499, want: false},
		{name: "server error remains uncertain", status: http.StatusBadGateway, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, taskHTTPResponseIsExplicitRejection(test.status))
		})
	}
}

func TestTaskResponseExplicitRejectionRequiresStructuredMarker(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "transport reset", err: errors.New("connection reset by peer"), want: false},
		{name: "parse failure", err: errors.New("invalid JSON"), want: false},
		{name: "structured provider rejection", err: service.ExplicitTaskSubmissionRejection(errors.New("validation failed")), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, taskResponseIsExplicitRejection(&dto.TaskError{Error: test.err}))
		})
	}
}

func TestTaskSubmissionUnknownResponseDoesNotExposeCause(t *testing.T) {
	secret := "sk-secret-in-transport-url"
	taskErr := taskSubmissionStateUnknown(&relaycommon.RelayInfo{RequestId: "request-safe-id"},
		errors.New("POST https://user:"+secret+"@provider.example failed; Authorization: Bearer "+secret))
	assert.Equal(t, "accepted_state_unknown", taskErr.Code)
	assert.Contains(t, taskErr.Message, "do not retry")
	assert.Contains(t, taskErr.Message, "contact an administrator")
	assert.Contains(t, taskErr.Message, "request-safe-id")
	assert.False(t, strings.Contains(taskErr.Message, secret))
	assert.Contains(t, taskErr.Error.Error(), secret, "the private cause remains available for sanitized logging")
}
