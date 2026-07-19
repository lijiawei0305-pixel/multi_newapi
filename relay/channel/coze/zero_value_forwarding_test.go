package coze

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCozeConversionPreservesExplicitFalseStream(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := convertCozeChatRequest(c, dto.GeneralOpenAIRequest{Stream: common.GetPointer(false)})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "stream").Exists())
	assert.False(t, gjson.GetBytes(encoded, "stream").Bool())
}
