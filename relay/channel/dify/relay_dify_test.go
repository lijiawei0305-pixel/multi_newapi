package dify

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2DifyDefaultUserIsValidJSONString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted := requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{})

	assert.NotEmpty(t, converted.User)
	_, err := common.Marshal(converted)
	require.NoError(t, err)

	converted = requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{User: json.RawMessage(`"client-user"`)})
	assert.Equal(t, "client-user", converted.User)
}
