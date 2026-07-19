package types

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestHideErrorDebugLogDoesNotExposeOriginalURLOrQuery(t *testing.T) {
	previousDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() {
		common.DebugEnabled = previousDebug
	})

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previousStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = previousStdout
		_ = reader.Close()
		_ = writer.Close()
	})

	secret := "https://provider.example/v1/models?api_key=must-not-leak"
	apiErr := NewError(errors.New(secret), ErrorCodeDoRequestFailed, ErrOptionWithHideErrMsg("upstream request failed"))
	require.EqualError(t, apiErr, "upstream request failed")
	require.NoError(t, writer.Close())
	os.Stdout = previousStdout
	output, err := io.ReadAll(reader)
	require.NoError(t, err)

	require.Contains(t, string(output), `replacement="upstream request failed"`)
	require.Contains(t, string(output), "origin_error_type=*errors.errorString")
	require.NotContains(t, string(output), "provider.example")
	require.NotContains(t, string(output), "api_key")
	require.NotContains(t, string(output), "must-not-leak")
}
