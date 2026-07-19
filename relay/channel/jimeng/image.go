package jimeng

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type ImageResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BinaryDataBase64 []string `json:"binary_data_base64"`
		ImageUrls        []string `json:"image_urls"`
		RephraseResult   string   `json:"rephraser_result"`
		RequestID        string   `json:"request_id"`
		// Other fields are omitted for brevity
	} `json:"data"`
	RequestID   string `json:"request_id"`
	Status      int    `json:"status"`
	TimeElapsed string `json:"time_elapsed"`
}

func responseJimeng2OpenAIImage(_ *gin.Context, response *ImageResponse, info *relaycommon.RelayInfo) *dto.ImageResponse {
	imageResponse := dto.ImageResponse{
		Created: info.StartTime.Unix(),
	}

	for _, base64Data := range response.Data.BinaryDataBase64 {
		imageResponse.Data = append(imageResponse.Data, dto.ImageData{
			B64Json: base64Data,
		})
	}
	for _, imageUrl := range response.Data.ImageUrls {
		imageResponse.Data = append(imageResponse.Data, dto.ImageData{
			Url: imageUrl,
		})
	}

	return &imageResponse
}

// jimengImageHandler handles the Jimeng image generation response
func jimengImageHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var jimengResponse ImageResponse
	defer service.CloseResponseBodyGracefully(resp)
	if err := common.DecodeJsonWithLimit(resp.Body, &jimengResponse, common.UpstreamJSONBodyLimit()); err != nil {
		service.MarkUpstreamAccepted(c)
		if errors.Is(err, common.ErrReadLimitExceeded) {
			common.SysError("accepted Jimeng image response exceeded the local response limit; retry suppressed")
		} else {
			common.SysError(fmt.Sprintf("accepted Jimeng image response decode failed: error_type=%T", err))
		}
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}

	// Check if the response indicates an error
	if jimengResponse.Code != 10000 {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: jimengResponse.Message,
			Type:    "jimeng_error",
			Param:   "",
			Code:    fmt.Sprintf("%d", jimengResponse.Code),
		}, http.StatusBadGateway)
	}
	service.MarkUpstreamAccepted(c)

	// Convert Jimeng response to OpenAI format
	fullTextResponse := responseJimeng2OpenAIImage(c, &jimengResponse, info)
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if err := channel.WriteImageResponse(c.Writer, fullTextResponse); err != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return &dto.Usage{}, nil
	}

	return &dto.Usage{}, nil
}
