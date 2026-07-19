package jimeng

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestJimengImageExtraFieldsPreserveExplicitZeroAndFalse(t *testing.T) {
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"jimeng",
		"prompt":"hello",
		"response_format":"b64_json",
		"extra_fields":{
			"seed":0,"width":0,"height":0,
			"use_pre_llm":false,"use_sr":false,"return_url":false,
			"logo_info":{"add_logo":false,"position":0,"language":0,"opacity":0}
		}
	}`), &request))

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, nil, request)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{
		"seed", "width", "height", "use_pre_llm", "use_sr", "return_url",
		"logo_info.add_logo", "logo_info.position", "logo_info.language", "logo_info.opacity",
	} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}
