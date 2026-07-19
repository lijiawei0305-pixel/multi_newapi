package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

func CovertMjpActionToModelName(mjAction string) string {
	modelName := "mj_" + strings.ToLower(mjAction)
	if mjAction == constant.MjActionSwapFace {
		modelName = "swap_face"
	}
	return modelName
}

func GetMjRequestModel(relayMode int, midjRequest *dto.MidjourneyRequest) (string, *dto.MidjourneyResponse, bool) {
	action := ""
	if relayMode == relayconstant.RelayModeMidjourneyAction {
		// plus request
		err := CoverPlusActionToNormalAction(midjRequest)
		if err != nil {
			return "", err, false
		}
		action = midjRequest.Action
	} else {
		switch relayMode {
		case relayconstant.RelayModeMidjourneyImagine:
			action = constant.MjActionImagine
		case relayconstant.RelayModeMidjourneyVideo:
			action = constant.MjActionVideo
		case relayconstant.RelayModeMidjourneyEdits:
			action = constant.MjActionEdits
		case relayconstant.RelayModeMidjourneyDescribe:
			action = constant.MjActionDescribe
		case relayconstant.RelayModeMidjourneyBlend:
			action = constant.MjActionBlend
		case relayconstant.RelayModeMidjourneyShorten:
			action = constant.MjActionShorten
		case relayconstant.RelayModeMidjourneyChange:
			action = midjRequest.Action
		case relayconstant.RelayModeMidjourneyModal:
			action = constant.MjActionModal
		case relayconstant.RelayModeSwapFace:
			action = constant.MjActionSwapFace
		case relayconstant.RelayModeMidjourneyUpload:
			action = constant.MjActionUpload
		case relayconstant.RelayModeMidjourneySimpleChange:
			params := ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return "", MidjourneyErrorWrapper(constant.MjRequestError, "invalid_request"), false
			}
			action = params.Action
		case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition, relayconstant.RelayModeMidjourneyNotify:
			return "", nil, true
		default:
			return "", MidjourneyErrorWrapper(constant.MjRequestError, "unknown_relay_action"), false
		}
	}
	modelName := CovertMjpActionToModelName(action)
	return modelName, nil, true
}

func CoverPlusActionToNormalAction(midjRequest *dto.MidjourneyRequest) *dto.MidjourneyResponse {
	if midjRequest == nil {
		return MidjourneyErrorWrapper(constant.MjRequestError, "invalid_request")
	}
	// "customId": "MJ::JOB::upsample::2::3dbbd469-36af-4a0f-8f02-df6c579e7011"
	customId := midjRequest.CustomId
	if customId == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "custom_id_is_required")
	}
	splits := strings.Split(customId, "::")
	if len(splits) < 2 {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	var action string
	if splits[1] == "JOB" {
		if len(splits) < 3 {
			return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
		}
		action = splits[2]
	} else {
		action = splits[1]
	}

	if action == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	if strings.Contains(action, "upsample") {
		if len(splits) < 4 {
			return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
		}
		index, err := strconv.Atoi(splits[3])
		if err != nil {
			return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
		}
		midjRequest.Index = index
		midjRequest.Action = constant.MjActionUpscale
	} else if strings.Contains(action, "variation") {
		midjRequest.Index = 1
		if action == "variation" {
			if len(splits) < 4 {
				return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
			}
			index, err := strconv.Atoi(splits[3])
			if err != nil {
				return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
			}
			midjRequest.Index = index
			midjRequest.Action = constant.MjActionVariation
		} else if action == "low_variation" {
			midjRequest.Action = constant.MjActionLowVariation
		} else if action == "high_variation" {
			midjRequest.Action = constant.MjActionHighVariation
		}
	} else if strings.Contains(action, "pan") {
		midjRequest.Action = constant.MjActionPan
		midjRequest.Index = 1
	} else if strings.Contains(action, "reroll") {
		midjRequest.Action = constant.MjActionReRoll
		midjRequest.Index = 1
	} else if action == "Outpaint" {
		midjRequest.Action = constant.MjActionZoom
		midjRequest.Index = 1
	} else if action == "CustomZoom" {
		midjRequest.Action = constant.MjActionCustomZoom
		midjRequest.Index = 1
	} else if action == "Inpaint" {
		midjRequest.Action = constant.MjActionInPaint
		midjRequest.Index = 1
	} else {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action:"+customId)
	}
	return nil
}

func ConvertSimpleChangeParams(content string) *dto.MidjourneyRequest {
	split := strings.Fields(content)
	if len(split) != 2 {
		return nil
	}

	action := strings.ToLower(split[1])
	if split[0] == "" || action == "" {
		return nil
	}
	changeParams := &dto.MidjourneyRequest{}
	changeParams.TaskId = split[0]

	if action == "r" {
		changeParams.Action = "REROLL"
		return changeParams
	}
	if len(action) < 2 {
		return nil
	}
	if action[0] == 'u' {
		changeParams.Action = "UPSCALE"
	} else if action[0] == 'v' {
		changeParams.Action = "VARIATION"
	} else {
		return nil
	}

	index, err := strconv.Atoi(action[1:2])
	if err != nil || index < 1 || index > 4 {
		return nil
	}
	changeParams.Index = index
	return changeParams
}

const midjourneyTaskPollingTimeout = 15 * time.Second

// FetchMidjourneyTasks polls one Midjourney channel under the caller's lease
// context. The finite timeout is a secondary bound for callers without a
// deadline; cancellation still takes precedence when the system-task lease is
// lost.
func FetchMidjourneyTasks(ctx context.Context, baseURL string, key string, taskIDs []string) ([]dto.MidjourneyDto, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, midjourneyTaskPollingTimeout)
	defer cancel()

	body, err := common.Marshal(map[string]any{"ids": taskIDs})
	if err != nil {
		return nil, fmt.Errorf("marshal Midjourney task poll request: %w", err)
	}
	requestURL := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/mj/task/list-by-condition"
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Midjourney task poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("mj-api-secret", key)

	client := GetHttpClient()
	if client == nil {
		return nil, fmt.Errorf("Midjourney task poll HTTP client is not initialized")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Midjourney task poll request: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Midjourney task poll returned an empty response")
	}
	defer CloseResponseBodyGracefully(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Midjourney task poll returned status %d", resp.StatusCode)
	}

	responseBody, err := common.ReadAllWithLimit(resp.Body, upstreamControlPlaneResponseMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("read Midjourney task poll response: %w", err)
	}
	var responseItems []dto.MidjourneyDto
	if err := common.Unmarshal(responseBody, &responseItems); err != nil {
		return nil, fmt.Errorf("decode Midjourney task poll response_%s: %w", logger.PayloadMetadata(responseBody), err)
	}
	return responseItems, nil
}

func DoMidjourneyHttpRequest(c *gin.Context, timeout time.Duration, fullRequestURL string) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var nullBytes []byte
	var mapResult map[string]interface{}
	// if get request, no need to read request body
	if c.Request.Method != "GET" {
		err := common.DecodeJson(c.Request.Body, &mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		if !setting.MjAccountFilterEnabled {
			delete(mapResult, "accountFilter")
		}
		if !setting.MjNotifyEnabled {
			delete(mapResult, "notifyHook")
		}
	}
	if setting.MjModeClearEnabled {
		if prompt, ok := mapResult["prompt"].(string); ok {
			prompt = strings.Replace(prompt, "--fast", "", -1)
			prompt = strings.Replace(prompt, "--relax", "", -1)
			prompt = strings.Replace(prompt, "--turbo", "", -1)

			mapResult["prompt"] = prompt
		}
	}
	reqBody, err := common.Marshal(mapResult)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "marshal_request_body_failed", http.StatusInternalServerError), nullBytes, err
	}
	requestContext := c.Request.Context()
	if requestContext == nil {
		requestContext = context.Background()
	}
	ctx, cancel := context.WithTimeout(requestContext, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, c.Request.Method, fullRequestURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "create_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	auth := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if auth != "" {
		auth = strings.TrimPrefix(auth, "Bearer ")
		req.Header.Set("mj-api-secret", auth)
	}
	client := GetHttpClient()
	if client == nil {
		err = fmt.Errorf("HTTP client is not initialized")
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "http_client_not_initialized", http.StatusInternalServerError), nullBytes, err
	}
	resp, err := client.Do(req)
	if err != nil {
		common.SysLog(fmt.Sprintf("do request failed: error_type=%T", err))
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "do_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	if resp == nil {
		err = fmt.Errorf("upstream returned an empty response")
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response", http.StatusBadGateway), nullBytes, err
	}
	defer CloseResponseBodyGracefully(resp)
	statusCode := resp.StatusCode
	err = req.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	err = c.Request.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	var midjResponse dto.MidjourneyResponse
	var midjourneyUploadsResponse dto.MidjourneyUploadResponse
	responseBody, err := common.ReadAllWithLimit(resp.Body, upstreamControlPlaneResponseMaxBytes)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_response_body_failed", statusCode), nullBytes, err
	}
	logger.LogPayload(c, "Midjourney response body", responseBody)
	if len(responseBody) == 0 {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response_body", statusCode), responseBody, nil
	} else {
		err = common.Unmarshal(responseBody, &midjResponse)
		if err != nil {
			err2 := common.Unmarshal(responseBody, &midjourneyUploadsResponse)
			if err2 != nil {
				return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "unmarshal_response_body_failed", statusCode), responseBody, err
			}
		}
	}
	//for k, v := range resp.Header {
	//	c.Writer.Header().Set(k, v[0])
	//}
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   midjResponse,
	}, responseBody, nil
}
