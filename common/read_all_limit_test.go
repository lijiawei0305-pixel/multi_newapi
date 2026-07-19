package common

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadAllWithLimitBoundary(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		limit   int64
		wantErr bool
	}{
		{name: "empty at zero", body: "", limit: 0},
		{name: "exact limit", body: "1234", limit: 4},
		{name: "one byte over", body: "12345", limit: 4, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := ReadAllWithLimit(strings.NewReader(test.body), test.limit)
			if !test.wantErr {
				require.NoError(t, err)
				assert.Equal(t, test.body, string(body))
				return
			}
			require.ErrorIs(t, err, ErrReadLimitExceeded)
			var limitErr *ReadLimitExceededError
			require.ErrorAs(t, err, &limitErr)
			assert.Equal(t, test.limit, limitErr.Limit)
			assert.Nil(t, body, "oversized partial data must not be processed")
		})
	}
}

func TestReadAllWithLimitUsesUpstreamJSONDefault(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "1")
	body, err := ReadAllWithLimit(strings.NewReader("1234"))
	require.NoError(t, err)
	assert.Equal(t, "1234", string(body))
	_, err = ReadAllWithLimit(strings.NewReader("12345"))
	require.True(t, errors.Is(err, ErrReadLimitExceeded))
}

func TestUpstreamJSONBodyLimitDefault(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "")
	assert.Equal(t, defaultUpstreamJSONMaxBytes, UpstreamJSONBodyLimit())

	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "0")
	assert.Equal(t, defaultUpstreamJSONMaxBytes, UpstreamJSONBodyLimit())
}

func TestDecodeJsonWithLimitBoundariesAndTrailingInput(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	exact := `{"value":"ok"}`
	var decoded payload
	require.NoError(t, DecodeJsonWithLimit(strings.NewReader(exact), &decoded, int64(len(exact))))
	assert.Equal(t, "ok", decoded.Value)

	decoded = payload{}
	withWhitespace := exact + " \n\t"
	require.NoError(t, DecodeJsonWithLimit(strings.NewReader(withWhitespace), &decoded, int64(len(withWhitespace))))
	assert.Equal(t, "ok", decoded.Value)

	decoded = payload{}
	err := DecodeJsonWithLimit(strings.NewReader(withWhitespace), &decoded, int64(len(withWhitespace)-1))
	require.ErrorIs(t, err, ErrReadLimitExceeded)
	var limitErr *ReadLimitExceededError
	require.ErrorAs(t, err, &limitErr)
	assert.Equal(t, int64(len(withWhitespace)-1), limitErr.Limit)

	err = DecodeJsonWithLimit(strings.NewReader(exact+` {"value":"second"}`), &decoded, 128)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrReadLimitExceeded)
	assert.Contains(t, err.Error(), "multiple values")
}

func TestCopyWithLimitExactAndProbeByteRequiresDiscard(t *testing.T) {
	var destination bytes.Buffer
	written, err := CopyWithLimit(&destination, strings.NewReader("1234"), 4)
	require.NoError(t, err)
	assert.Equal(t, int64(4), written)
	assert.Equal(t, "1234", destination.String())

	destination.Reset()
	written, err = CopyWithLimit(&destination, strings.NewReader("12345"), 4)
	require.ErrorIs(t, err, ErrReadLimitExceeded)
	assert.Equal(t, int64(5), written)
	assert.Equal(t, "12345", destination.String(), "the private destination receives one probe byte")
	var limitErr *ReadLimitExceededError
	require.ErrorAs(t, err, &limitErr)
	assert.Equal(t, int64(4), limitErr.Limit)
	destination.Reset()
	assert.Empty(t, destination.Bytes(), "caller discard removes the private probe byte")
}
