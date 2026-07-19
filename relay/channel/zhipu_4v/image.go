package zhipu_4v

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type zhipuImageRequest struct {
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	Quality          string `json:"quality,omitempty"`
	Size             string `json:"size,omitempty"`
	WatermarkEnabled *bool  `json:"watermark_enabled,omitempty"`
	UserID           string `json:"user_id,omitempty"`
}

type zhipuImageResponse struct {
	Created       *int64            `json:"created,omitempty"`
	Data          []zhipuImageData  `json:"data,omitempty"`
	ContentFilter any               `json:"content_filter,omitempty"`
	Usage         *dto.Usage        `json:"usage,omitempty"`
	Error         *zhipuImageError  `json:"error,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	ExtendParam   map[string]string `json:"extendParam,omitempty"`
}

type zhipuImageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type zhipuImageData struct {
	Url      string `json:"url,omitempty"`
	ImageUrl string `json:"image_url,omitempty"`
	B64Json  string `json:"b64_json,omitempty"`
	B64Image string `json:"b64_image,omitempty"`
}

type openAIImagePayload struct {
	Created int64             `json:"created"`
	Data    []openAIImageData `json:"data"`
}

type openAIImageData struct {
	B64Json string `json:"b64_json"`
}

func zhipu4vImageHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var zhipuResp zhipuImageResponse
	if err := common.DecodeJsonWithLimit(resp.Body, &zhipuResp, common.UpstreamJSONBodyLimit()); err != nil {
		service.MarkUpstreamAccepted(c)
		common.SysError(fmt.Sprintf("accepted Zhipu image response decode failed: error_type=%T", err))
		return nil, channel.AcceptedResponseDeliveryError()
	}

	if zhipuResp.Error != nil && (zhipuResp.Error.Message != "" || zhipuResp.Error.Code != "") {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: zhipuResp.Error.Message,
			Type:    "zhipu_image_error",
			Code:    zhipuResp.Error.Code,
		}, http.StatusBadGateway)
	}
	service.MarkUpstreamAccepted(c)

	payload := dto.ImageResponse{}
	budget := service.NewImageResponseEncodedBudget()
	if zhipuResp.Created != nil && *zhipuResp.Created != 0 {
		payload.Created = *zhipuResp.Created
	} else {
		payload.Created = info.StartTime.Unix()
	}
	for _, data := range zhipuResp.Data {
		var b64 string
		switch {
		case data.B64Json != "":
			b64 = data.B64Json
		case data.B64Image != "":
			b64 = data.B64Image
		default:
			url := data.Url
			if url == "" {
				url = data.ImageUrl
			}
			if url == "" {
				logger.LogWarn(c, "zhipu_image_missing_data")
				continue
			}
			downloadContext := context.Background()
			if c != nil && c.Request != nil {
				downloadContext = c.Request.Context()
			}
			_, downloaded, err := service.GetImageFromURLContextWithLimit(downloadContext, url, budget.RemainingRawBytes())
			if err != nil {
				logger.LogError(c, "zhipu_image_get_b64_failed: "+err.Error())
				continue
			}
			b64 = downloaded
		}

		if b64 == "" {
			logger.LogWarn(c, "zhipu_image_empty_b64")
			continue
		}
		if err := budget.ConsumeBase64(b64); err != nil {
			logger.LogError(c, "zhipu_image_response_budget_failed: "+err.Error())
			continue
		}

		imageData := dto.ImageData{
			B64Json: b64,
		}
		payload.Data = append(payload.Data, imageData)
	}
	if len(payload.Data) == 0 {
		return nil, channel.AcceptedResponseDeliveryError()
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if err := channel.WriteImageResponse(c.Writer, &payload); err != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}

	return &dto.Usage{}, nil
}
