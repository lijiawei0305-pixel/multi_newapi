package payment

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapCreateErrorNoAuth(t *testing.T) {
	raw := NewOutcomeError(CreateOutcomeDefinitiveReject, "platform_no_auth", "response",
		fmt.Errorf("wxpay: status=403 code=NO_AUTH"))
	got := MapCreateError(raw)
	assert.Equal(t, CodeProviderNoAuth, apperr.CodeOf(got))
	assert.Equal(t, 400, apperr.HTTPStatusOf(got))
}

func TestMapCreateErrorUnknown(t *testing.T) {
	raw := NewOutcomeError(CreateOutcomeUnknown, "timeout", "tls", fmt.Errorf("timeout"))
	got := MapCreateError(raw)
	require.ErrorIs(t, got, ErrCreateOutcomeUnknown)
	assert.Equal(t, CodeCreateOutcomeUnknown, apperr.CodeOf(got))
}

func TestMapCreateErrorOtherReject(t *testing.T) {
	raw := NewOutcomeError(CreateOutcomeDefinitiveReject, "platform_param_error", "response",
		fmt.Errorf("wxpay: PARAM_ERROR"))
	got := MapCreateError(raw)
	assert.Equal(t, CodeProviderReject, apperr.CodeOf(got))
	assert.Equal(t, 400, apperr.HTTPStatusOf(got))
}
