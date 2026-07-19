package channel

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

func TestWriteImageResponseMatchesCanonicalJSON(t *testing.T) {
	response := &dto.ImageResponse{
		Created: 123,
		Data: []dto.ImageData{
			{Url: "https://example.com/a?x=1&y=2", RevisedPrompt: "draw \"this\""},
			{B64Json: "ABC123+/="},
		},
		Metadata: json.RawMessage(`{"seed":42}`),
	}
	expected, err := common.Marshal(response)
	require.NoError(t, err)
	var actual bytes.Buffer

	require.NoError(t, WriteImageResponse(&actual, response))
	require.JSONEq(t, string(expected), actual.String())
}
