package common

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

func TestWriteJSONStringMatchesMarshalWithoutWholeValueBuffer(t *testing.T) {
	values := []string{
		"plain-base64-ABC123+/=",
		"quotes \" slash \\ controls\n\t",
		"html <script>& unicode 世界 \u2028",
		string([]byte{'a', 0xff, 'b'}),
	}
	for _, value := range values {
		expected, err := Marshal(value)
		require.NoError(t, err)
		var actual bytes.Buffer
		require.NoError(t, WriteJSONString(&actual, value))
		require.Equal(t, string(expected), actual.String())
	}
}

func TestEncodeJsonPreservesEncoderNewline(t *testing.T) {
	var buffer bytes.Buffer
	require.NoError(t, EncodeJson(&buffer, map[string]int{"value": 1}))
	require.Equal(t, "{\"value\":1}\n", buffer.String())
}
