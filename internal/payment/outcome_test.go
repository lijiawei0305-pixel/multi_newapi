package payment

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyNetworkErrorWroteRequest(t *testing.T) {
	err := errors.New("EOF")
	oe := ClassifyNetworkError(err, true)
	assert.Equal(t, CreateOutcomeUnknown, oe.Outcome)
}

func TestClassifyNetworkErrorTLSCert(t *testing.T) {
	err := errors.New("x509: certificate signed by unknown authority")
	oe := ClassifyNetworkError(err, false)
	assert.Equal(t, CreateOutcomeDefinitiveReject, oe.Outcome)
	assert.Equal(t, "tls_cert", oe.ErrorClass)
}

func TestClassifyNetworkErrorCanceled(t *testing.T) {
	oe := ClassifyNetworkError(context.Canceled, false)
	assert.Equal(t, "canceled", oe.ErrorClass)
}

func TestClassifyNetworkErrorTimeoutPreWrite(t *testing.T) {
	oe := ClassifyNetworkError(context.DeadlineExceeded, false)
	assert.Equal(t, CreateOutcomeUnknown, oe.Outcome)
}

func TestAsOutcomeWrapsBareError(t *testing.T) {
	oe := AsOutcome(errors.New("boom"))
	assert.Equal(t, CreateOutcomeUnknown, oe.Outcome)
}

func TestConnectRefusedClass(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	assert.Equal(t, "connect_refused", classifyErrorClass(err))
}
