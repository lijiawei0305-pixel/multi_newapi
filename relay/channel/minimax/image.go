package minimax

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type MiniMaxImageRequest struct {
	Model           string `json:"model"`
	Prompt          string `json:"prompt"`
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	ResponseFormat  string `json:"response_format,omitempty"`
	N               *int   `json:"n,omitempty"`
	PromptOptimizer *bool  `json:"prompt_optimizer,omitempty"`
	AigcWatermark   *bool  `json:"aigc_watermark,omitempty"`
}

type MiniMaxImageResponse struct {
	ID   string `json:"id"`
	Data struct {
		ImageURLs   []string `json:"image_urls"`
		ImageBase64 []string `json:"image_base64"`
	} `json:"data"`
	Metadata map[string]any `json:"metadata"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

func oaiImage2MiniMaxImageRequest(request dto.ImageRequest) MiniMaxImageRequest {
	responseFormat := normalizeMiniMaxResponseFormat(request.ResponseFormat)
	minimaxRequest := MiniMaxImageRequest{
		Model:          request.Model,
		Prompt:         request.Prompt,
		ResponseFormat: responseFormat,
		AigcWatermark:  request.Watermark,
	}

	if request.Model == "" {
		minimaxRequest.Model = "image-01"
	}
	if request.N != nil {
		n := common.SaturatingUintToInt(*request.N)
		minimaxRequest.N = &n
	}
	if aspectRatio := aspectRatioFromImageRequest(request); aspectRatio != "" {
		minimaxRequest.AspectRatio = aspectRatio
	}
	if raw, ok := request.Extra["prompt_optimizer"]; ok {
		var promptOptimizer bool
		if err := common.Unmarshal(raw, &promptOptimizer); err == nil {
			minimaxRequest.PromptOptimizer = &promptOptimizer
		}
	}

	return minimaxRequest
}

func aspectRatioFromImageRequest(request dto.ImageRequest) string {
	if raw, ok := request.Extra["aspect_ratio"]; ok {
		var aspectRatio string
		if err := common.Unmarshal(raw, &aspectRatio); err == nil && aspectRatio != "" {
			return aspectRatio
		}
	}

	switch request.Size {
	case "1024x1024":
		return "1:1"
	case "1792x1024":
		return "16:9"
	case "1024x1792":
		return "9:16"
	case "1536x1024", "1248x832":
		return "3:2"
	case "1024x1536", "832x1248":
		return "2:3"
	case "1152x864":
		return "4:3"
	case "864x1152":
		return "3:4"
	case "1344x576":
		return "21:9"
	}

	width, height, ok := parseImageSize(request.Size)
	if !ok {
		return ""
	}
	ratio := reduceAspectRatio(width, height)
	switch ratio {
	case "1:1", "16:9", "4:3", "3:2", "2:3", "3:4", "9:16", "21:9":
		return ratio
	default:
		return ""
	}
}

func parseImageSize(size string) (int, int, bool) {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func reduceAspectRatio(width, height int) string {
	divisor := gcd(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func normalizeMiniMaxResponseFormat(responseFormat string) string {
	switch strings.ToLower(responseFormat) {
	case "", "url":
		return "url"
	case "b64_json", "base64":
		return "base64"
	default:
		return responseFormat
	}
}

func responseMiniMax2OpenAIImage(response *MiniMaxImageResponse, info *relaycommon.RelayInfo) (*dto.ImageResponse, error) {
	imageResponse := &dto.ImageResponse{
		Created: info.StartTime.Unix(),
	}

	for _, imageURL := range response.Data.ImageURLs {
		imageResponse.Data = append(imageResponse.Data, dto.ImageData{Url: imageURL})
	}
	for _, imageBase64 := range response.Data.ImageBase64 {
		imageResponse.Data = append(imageResponse.Data, dto.ImageData{B64Json: imageBase64})
	}
	if len(response.Metadata) > 0 {
		metadata, err := common.Marshal(response.Metadata)
		if err != nil {
			return nil, err
		}
		imageResponse.Metadata = metadata
	}

	return imageResponse, nil
}

func miniMaxImageHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var minimaxResponse MiniMaxImageResponse
	if err := common.DecodeJsonWithLimit(resp.Body, &minimaxResponse, common.UpstreamJSONBodyLimit()); err != nil {
		service.MarkUpstreamAccepted(c)
		if errors.Is(err, common.ErrReadLimitExceeded) {
			common.SysError("accepted MiniMax image response exceeded the local response limit; retry suppressed")
		} else {
			common.SysError(fmt.Sprintf("accepted MiniMax image response decode failed: error_type=%T", err))
		}
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return nil, channel.AcceptedResponseDeliveryError()
	}
	if minimaxResponse.BaseResp.StatusCode != 0 {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: minimaxResponse.BaseResp.StatusMsg,
			Type:    "minimax_image_error",
			Code:    fmt.Sprintf("%d", minimaxResponse.BaseResp.StatusCode),
		}, http.StatusBadGateway)
	}
	service.MarkUpstreamAccepted(c)

	openAIResponse, err := responseMiniMax2OpenAIImage(&minimaxResponse, info)
	if err != nil {
		common.SysError(fmt.Sprintf("accepted MiniMax image response conversion failed: error_type=%T", err))
		return nil, channel.AcceptedResponseDeliveryError()
	}
	budget := service.NewImageResponseEncodedBudget()
	if err := budget.ReserveEncodedBytes(int64(len(openAIResponse.Metadata))); err != nil {
		common.SysError(fmt.Sprintf("accepted MiniMax image metadata exceeded cumulative encoded budget: error_type=%T", err))
		return nil, channel.AcceptedResponseDeliveryError()
	}
	usableData := make([]dto.ImageData, 0, len(openAIResponse.Data))
	for _, image := range openAIResponse.Data {
		if image.Url == "" && image.B64Json == "" {
			continue
		}
		if image.B64Json == "" {
			usableData = append(usableData, image)
			continue
		}
		if err := budget.ConsumeBase64(image.B64Json); err != nil {
			common.SysError(fmt.Sprintf("accepted MiniMax image response exceeded cumulative encoded budget: error_type=%T", err))
			return nil, channel.AcceptedResponseDeliveryError()
		}
		usableData = append(usableData, image)
	}
	if len(usableData) == 0 {
		return nil, channel.AcceptedResponseDeliveryError()
	}
	openAIResponse.Data = usableData
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if err := channel.WriteImageResponse(c.Writer, openAIResponse); err != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return &dto.Usage{}, nil
	}

	return &dto.Usage{}, nil
}
