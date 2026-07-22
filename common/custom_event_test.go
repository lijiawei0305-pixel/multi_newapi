package common

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingCustomEventWriter struct {
	err error
}

func (w failingCustomEventWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestCustomEventRenderPreservesSSEFraming(t *testing.T) {
	recorder := httptest.NewRecorder()
	event := CustomEvent{Data: "data: {\"ok\":true}\r\n"}

	require.NoError(t, event.Render(recorder))

	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "data: {\"ok\":true}\\r\n\n\n", recorder.Body.String())
}

func TestCustomEventRenderReturnsWriterFailure(t *testing.T) {
	wantErr := errors.New("write failed")

	err := writeData(checkWriter(failingCustomEventWriter{err: wantErr}), "data: value")

	require.ErrorIs(t, err, wantErr)
}
