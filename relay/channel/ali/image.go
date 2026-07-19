package ali

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func oaiImage2AliImageRequest(info *relaycommon.RelayInfo, request dto.ImageRequest, isSync bool) (*AliImageRequest, error) {
	var imageRequest AliImageRequest
	imageRequest.Model = request.Model
	imageRequest.ResponseFormat = request.ResponseFormat
	if request.Extra != nil {
		if val, ok := request.Extra["parameters"]; ok {
			err := common.Unmarshal(val, &imageRequest.Parameters)
			if err != nil {
				return nil, fmt.Errorf("invalid parameters field: %w", err)
			}
		} else {
			// 兼容没有parameters字段的情况，从openai标准字段中提取参数
			imageRequest.Parameters = AliImageParameters{
				Size:      strings.Replace(request.Size, "x", "*", -1),
				Watermark: request.Watermark,
			}
			if request.N != nil {
				n := common.SaturatingUintToInt(*request.N)
				imageRequest.Parameters.N = &n
			}
		}
		if val, ok := request.Extra["input"]; ok {
			err := common.Unmarshal(val, &imageRequest.Input)
			if err != nil {
				return nil, fmt.Errorf("invalid input field: %w", err)
			}
		}
	}

	if strings.Contains(request.Model, "z-image") {
		// z-image 开启prompt_extend后，按2倍计费
		if imageRequest.Parameters.PromptExtendValue() {
			info.PriceData.AddOtherRatio("prompt_extend", 2)
		}
	}
	if imageRequest.Parameters.N != nil && *imageRequest.Parameters.N <= 0 {
		return nil, errors.New("parameters.n must be greater than zero")
	}

	if imageRequest.Parameters.N != nil && *imageRequest.Parameters.N > 0 {
		info.PriceData.AddOtherRatio("n", float64(*imageRequest.Parameters.N))
	}

	// 同步图片模型和异步图片模型请求格式不一样
	if isSync {
		if imageRequest.Input == nil {
			imageRequest.Input = AliImageInput{
				Messages: []AliMessage{
					{
						Role: "user",
						Content: []AliMediaContent{
							{
								Text: request.Prompt,
							},
						},
					},
				},
			}
		}
	} else {
		if imageRequest.Input == nil {
			imageRequest.Input = AliImageInput{
				Prompt: request.Prompt,
			}
		}
	}

	return &imageRequest, nil
}
func getImageBase64sFromForm(c *gin.Context, fieldName string) ([]string, error) {
	mf := c.Request.MultipartForm
	if mf == nil {
		if _, err := c.MultipartForm(); err != nil {
			return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
		}
		mf = c.Request.MultipartForm
	}

	var imageFiles []*multipart.FileHeader
	var exists bool

	// First check for standard "image" field
	if imageFiles, exists = mf.File["image"]; !exists || len(imageFiles) == 0 {
		// If not found, check for "image[]" field
		if imageFiles, exists = mf.File["image[]"]; !exists || len(imageFiles) == 0 {
			// If still not found, iterate through all fields to find any that start with "image["
			foundArrayImages := false
			for fieldName, files := range mf.File {
				if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
					foundArrayImages = true
					imageFiles = append(imageFiles, files...)
				}
			}

			// If no image fields found at all
			if !foundArrayImages && (len(imageFiles) == 0) {
				return nil, errors.New("image is required")
			}
		}
	}

	if len(imageFiles) == 0 {
		return nil, errors.New("image is required")
	}

	//if len(imageFiles) > 1 {
	//	return nil, errors.New("only one image is supported for qwen edit")
	//}

	// 获取base64编码的图片
	var imageBase64s []string
	for _, file := range imageFiles {
		image, err := file.Open()
		if err != nil {
			return nil, errors.New("failed to open image file")
		}

		// 读取文件内容
		imageData, err := io.ReadAll(image)
		if err != nil {
			return nil, errors.New("failed to read image file")
		}

		// 获取MIME类型
		mimeType := http.DetectContentType(imageData)

		// 编码为base64
		base64Data := base64.StdEncoding.EncodeToString(imageData)

		// 构造data URL格式
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
		imageBase64s = append(imageBase64s, dataURL)
		image.Close()
	}
	return imageBase64s, nil
}

func oaiFormEdit2AliImageEdit(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (*AliImageRequest, error) {
	var imageRequest AliImageRequest
	imageRequest.Model = request.Model
	imageRequest.ResponseFormat = request.ResponseFormat

	imageBase64s, err := getImageBase64sFromForm(c, "image")
	if err != nil {
		return nil, fmt.Errorf("get image base64s from form failed: %w", err)
	}
	//dto.MediaContent{}
	mediaContents := make([]AliMediaContent, len(imageBase64s))
	for i, b64 := range imageBase64s {
		mediaContents[i] = AliMediaContent{
			Image: b64,
		}
	}
	mediaContents = append(mediaContents, AliMediaContent{
		Text: request.Prompt,
	})
	imageRequest.Input = AliImageInput{
		Messages: []AliMessage{
			{
				Role:    "user",
				Content: mediaContents,
			},
		},
	}
	imageRequest.Parameters = AliImageParameters{
		Watermark: request.Watermark,
	}
	if request.N != nil {
		n := common.SaturatingUintToInt(*request.N)
		imageRequest.Parameters.N = &n
	}
	return &imageRequest, nil
}

func updateTask(ctx context.Context, info *relaycommon.RelayInfo, taskID string) (*AliResponse, error, []byte) {
	url := fmt.Sprintf("%s/api/v1/tasks/%s", info.ChannelBaseUrl, taskID)

	var aliResponse AliResponse

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &aliResponse, err, nil
	}

	req.Header.Set("Authorization", "Bearer "+info.ApiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		common.SysLog(fmt.Sprintf("updateTask client.Do failed: error_type=%T", err))
		return &aliResponse, err, nil
	}
	defer resp.Body.Close()

	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return &aliResponse, err, nil
	}

	var response AliResponse
	err = common.Unmarshal(responseBody, &response)
	if err != nil {
		common.SysLog("updateTask NewDecoder err: " + err.Error())
		return &aliResponse, err, nil
	}

	return &response, nil, responseBody
}

func asyncTaskWait(c *gin.Context, info *relaycommon.RelayInfo, taskID string) (*AliResponse, []byte, error) {
	waitSeconds := 10
	step := 0
	maxStep := 20

	var taskResponse AliResponse
	var responseBody []byte

	requestContext := context.Background()
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	timeoutSeconds := common.GetEnvOrDefault("RELAY_ALI_IMAGE_POLL_TIMEOUT_SECONDS", 210)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 210
	}
	ctx, cancel := context.WithTimeout(requestContext, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	service.MarkUpstreamAccepted(c)
	initialTimer := time.NewTimer(5 * time.Second)
	select {
	case <-ctx.Done():
		initialTimer.Stop()
		return nil, nil, ctx.Err()
	case <-initialTimer.C:
	}

	for {
		logger.LogDebug(c, "asyncTaskWait step %d/%d, wait %d seconds", step, maxStep, waitSeconds)
		step++
		rsp, err, body := updateTask(ctx, info, taskID)
		responseBody = body
		if err != nil {
			logger.LogWarn(c, "asyncTaskWait UpdateTask err: "+err.Error())
			if step >= maxStep {
				break
			}
			timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}

		if rsp.Output.TaskStatus == "" {
			return &taskResponse, responseBody, nil
		}

		switch rsp.Output.TaskStatus {
		case "FAILED":
			fallthrough
		case "CANCELED":
			fallthrough
		case "SUCCEEDED":
			fallthrough
		case "UNKNOWN":
			return rsp, responseBody, nil
		}
		if step >= maxStep {
			break
		}
		timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, nil, fmt.Errorf("aliAsyncTaskWait timeout")
}

func responseAli2OpenAIImage(c *gin.Context, response *AliResponse, originBody []byte, info *relaycommon.RelayInfo, responseFormat string) (*dto.ImageResponse, error) {
	imageResponse := dto.ImageResponse{
		Created: info.StartTime.Unix(),
	}
	budget := service.NewImageResponseEncodedBudget()
	if err := budget.ReserveEncodedBytes(int64(len(originBody))); err != nil {
		return nil, err
	}

	if len(response.Output.Results) > 0 {
		imageResponse.Data = response.Output.ResultToOpenAIImageDate(c, responseFormat, budget)
	} else if len(response.Output.Choices) > 0 {
		imageResponse.Data = response.Output.ChoicesToOpenAIImageDate(c, responseFormat, budget)
	}

	imageResponse.Metadata = originBody
	return &imageResponse, nil
}

func aliImageHandler(a *Adaptor, c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	if resp == nil || resp.Body == nil {
		return types.NewError(errors.New("Ali image response is unavailable"), types.ErrorCodeBadResponse), nil
	}
	defer service.CloseResponseBodyGracefully(resp)
	responseFormat := c.GetString("response_format")

	var aliTaskResponse AliResponse
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		common.SysError(fmt.Sprintf("accepted Ali image response read failed: error_type=%T", err))
		return channel.AcceptedResponseDeliveryError(), nil
	}
	err = common.Unmarshal(responseBody, &aliTaskResponse)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		common.SysError(fmt.Sprintf("accepted Ali image response decode failed: error_type=%T", err))
		return channel.AcceptedResponseDeliveryError(), nil
	}

	if aliTaskResponse.Message != "" || aliTaskResponse.Code != "" {
		logger.LogError(c, fmt.Sprintf("ali_async_task_failed code=%s message_%s", aliTaskResponse.Code, logger.PayloadMetadata([]byte(aliTaskResponse.Message))))
		message := aliTaskResponse.Message
		if message == "" {
			message = aliTaskResponse.Code
		}
		return types.NewError(errors.New(message), types.ErrorCodeBadResponse), nil
	}

	var (
		aliResponse    *AliResponse
		originRespBody []byte
	)

	if a.IsSyncImageModel {
		service.MarkUpstreamAccepted(c)
		aliResponse = &aliTaskResponse
		originRespBody = responseBody
	} else {
		// 异步图片模型需要轮询任务结果
		if strings.TrimSpace(aliTaskResponse.Output.TaskId) == "" {
			service.MarkUpstreamAccepted(c)
			return channel.AcceptedResponseDeliveryError(), nil
		}
		aliResponse, originRespBody, err = asyncTaskWait(c, info, aliTaskResponse.Output.TaskId)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponse), nil
		}
		if aliResponse.Output.TaskStatus != "SUCCEEDED" {
			return types.WithOpenAIError(types.OpenAIError{
				Message: aliResponse.Output.Message,
				Type:    "ali_error",
				Param:   "",
				Code:    aliResponse.Output.Code,
			}, resp.StatusCode), nil
		}
	}

	if a.IsSyncImageModel {
		logger.LogPayload(c, "Ali sync image result", originRespBody)
	} else {
		logger.LogPayload(c, "Ali async image result", originRespBody)
	}

	imageResponses, err := responseAli2OpenAIImage(c, aliResponse, originRespBody, info, responseFormat)
	if err != nil {
		common.SysError(fmt.Sprintf("accepted Ali image response exceeded cumulative encoded budget: error_type=%T", err))
		return channel.AcceptedResponseDeliveryError(), nil
	}
	if len(imageResponses.Data) == 0 {
		return channel.AcceptedResponseDeliveryError(), nil
	}
	if aliResponse.Usage.ImageCount != 0 {
		info.PriceData.AddOtherRatio("n", float64(aliResponse.Usage.ImageCount))
	} else if len(imageResponses.Data) != 0 {
		info.PriceData.AddOtherRatio("n", float64(len(imageResponses.Data)))
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if err := channel.WriteImageResponse(c.Writer, imageResponses); err != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(err)
		}
		return channel.AcceptedResponseDeliveryError(), nil
	}

	return nil, &dto.Usage{}
}
