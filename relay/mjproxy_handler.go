package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	agenthook "github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func RelayMidjourneyImage(c *gin.Context) {
	taskId := c.Param("id")
	midjourneyTask := model.GetByOnlyMJId(taskId)
	if midjourneyTask == nil {
		c.JSON(400, gin.H{
			"error": "midjourney_task_not_found",
		})
		return
	}
	var httpClient *http.Client
	if channel, err := model.CacheGetChannel(midjourneyTask.ChannelId); err == nil {
		proxy := channel.GetSetting().Proxy
		if proxy != "" {
			if httpClient, err = service.GetSSRFProtectedHttpClientWithProxy(proxy); err != nil {
				c.JSON(400, gin.H{
					"error": "proxy_url_invalid",
				})
				return
			}
		}
	}
	if httpClient == nil {
		var err error
		httpClient, err = service.GetSSRFProtectedHttpClientWithProxy("")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ssrf_client_unavailable"})
			return
		}
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(midjourneyTask.ImageUrl, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("request blocked: %v", err),
		})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, midjourneyTask.ImageUrl, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_image_url"})
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "http_get_image_failed",
		})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, readErr := common.ReadAllWithLimit(resp.Body, 4<<20)
		if readErr != nil {
			common.SysLog("midjourney image upstream error body was unavailable: " + readErr.Error())
		} else {
			common.SysLog("midjourney image upstream error response_" + common.PayloadMetadata(responseBody))
		}
		c.JSON(resp.StatusCode, gin.H{
			"error": "upstream_image_fetch_failed",
		})
		return
	}
	// 从Content-Type头获取MIME类型
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// 如果无法确定内容类型，则默认为jpeg
		contentType = "image/jpeg"
	}
	// 设置响应的内容类型
	c.Writer.Header().Set("Content-Type", contentType)
	// 将图片流式传输到响应体
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Println("Failed to stream image:", err)
	}
	return
}

func RelayMidjourneyNotify(c *gin.Context) *dto.MidjourneyResponse {
	var midjRequest dto.MidjourneyDto
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "bind_request_body_failed",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask := model.GetByOnlyMJId(midjRequest.MjId)
	if midjourneyTask == nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "midjourney_task_not_found",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask.Progress = midjRequest.Progress
	midjourneyTask.PromptEn = midjRequest.PromptEn
	midjourneyTask.State = midjRequest.State
	midjourneyTask.SubmitTime = midjRequest.SubmitTime
	midjourneyTask.StartTime = midjRequest.StartTime
	midjourneyTask.FinishTime = midjRequest.FinishTime
	midjourneyTask.ImageUrl = midjRequest.ImageUrl
	midjourneyTask.VideoUrl = midjRequest.VideoUrl
	videoUrlsStr, _ := common.Marshal(midjRequest.VideoUrls)
	midjourneyTask.VideoUrls = string(videoUrlsStr)
	midjourneyTask.Status = midjRequest.Status
	midjourneyTask.FailReason = midjRequest.FailReason
	err = midjourneyTask.Update()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "update_midjourney_task_failed",
		}
	}

	return nil
}

func coverMidjourneyTaskDto(c *gin.Context, originTask *model.Midjourney) (midjourneyTask dto.MidjourneyDto) {
	midjourneyTask.MjId = originTask.MjId
	midjourneyTask.Progress = originTask.Progress
	midjourneyTask.PromptEn = originTask.PromptEn
	midjourneyTask.State = originTask.State
	midjourneyTask.SubmitTime = originTask.SubmitTime
	midjourneyTask.StartTime = originTask.StartTime
	midjourneyTask.FinishTime = originTask.FinishTime
	midjourneyTask.ImageUrl = ""
	if originTask.ImageUrl != "" && setting.MjForwardUrlEnabled {
		midjourneyTask.ImageUrl = system_setting.ServerAddress + "/mj/image/" + originTask.MjId
		if originTask.Status != "SUCCESS" {
			midjourneyTask.ImageUrl += "?rand=" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	} else {
		midjourneyTask.ImageUrl = originTask.ImageUrl
	}
	if originTask.VideoUrl != "" {
		midjourneyTask.VideoUrl = originTask.VideoUrl
	}
	midjourneyTask.Status = originTask.Status
	midjourneyTask.FailReason = originTask.FailReason
	midjourneyTask.Action = originTask.Action
	midjourneyTask.Description = originTask.Description
	midjourneyTask.Prompt = originTask.Prompt
	if originTask.Buttons != "" {
		var buttons []dto.ActionButton
		err := common.Unmarshal([]byte(originTask.Buttons), &buttons)
		if err == nil {
			midjourneyTask.Buttons = buttons
		}
	}
	if originTask.VideoUrls != "" {
		var videoUrls []dto.ImgUrls
		err := common.Unmarshal([]byte(originTask.VideoUrls), &videoUrls)
		if err == nil {
			midjourneyTask.VideoUrls = videoUrls
		}
	}
	if originTask.Properties != "" {
		var properties dto.Properties
		err := common.Unmarshal([]byte(originTask.Properties), &properties)
		if err == nil {
			midjourneyTask.Properties = &properties
		}
	}
	return
}

func prepareMidjourneySubmissionRecovery(c *gin.Context, info *relaycommon.RelayInfo) (*common.BufferedResponseReservation, bool, *dto.MidjourneyResponse) {
	if strings.TrimSpace(info.RequestId) == "" {
		info.RequestId = common.GetUUID()
	}
	if strings.TrimSpace(info.PublicTaskID) == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}
	claimSpec, err := buildTaskSubmissionClaimSpec(c, info, model.TaskSubmissionKindMidjourney, info.PublicTaskID)
	if err != nil {
		return nil, false, midjourneyClaimFailure(info, taskSubmissionClaimState{}, err)
	}
	if existing, found, lookupErr := lookupTaskSubmission(c, info, claimSpec); found || lookupErr != nil {
		if lookupErr != nil {
			return nil, false, midjourneyClaimFailure(info, existing, lookupErr)
		}
		if existing.Replay {
			return nil, true, nil
		}
	}
	reservation, err := common.AcquireBufferedResponseReservation()
	if err != nil {
		return nil, false, service.MidjourneyErrorWrapper(constant.MjRequestError, "response_buffer_capacity_exhausted")
	}
	claim, err := claimTaskSubmission(c, info, claimSpec)
	if err != nil {
		reservation.Release()
		return nil, false, midjourneyClaimFailure(info, claim, err)
	}
	if claim.Replay {
		reservation.Release()
		return nil, true, nil
	}
	return reservation, false, nil
}

func midjourneyClaimFailure(info *relaycommon.RelayInfo, claim taskSubmissionClaimState, err error) *dto.MidjourneyResponse {
	if claim.Recovery != nil && claim.Recovery.Status == model.TaskSubmissionStatusUncertain {
		return midjourneySubmissionStateUnknown(info)
	}
	if errors.Is(err, model.ErrTaskSubmissionIdempotencyPayloadMismatch) ||
		errors.Is(err, model.ErrTaskSubmissionIdempotencyInProgress) || claim.Recovery != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "idempotency_conflict")
	}
	return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_idempotency_key")
}

func markMidjourneySubmissionUncertain(info *relaycommon.RelayInfo, provider string, initialQuota int) *dto.MidjourneyResponse {
	if err := model.MarkTaskSubmissionUncertainWithMetadata(info.RequestId, model.TaskSubmissionKindMidjourney,
		taskSubmissionAttemptMetadata(info, provider, initialQuota)); err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "mark_submission_uncertain_failed")
	}
	info.TaskSubmissionRecoveryProtected = true
	return nil
}

func rejectMidjourneySubmission(_ *gin.Context, info *relaycommon.RelayInfo, _ bool) bool {
	if err := model.MarkTaskSubmissionRejected(info.RequestId, model.TaskSubmissionKindMidjourney); err != nil {
		info.TaskSubmissionRecoveryProtected = true
		common.SysLog("midjourney explicit rejection state update failed: " + err.Error())
		return false
	}
	info.TaskSubmissionRecoveryProtected = false
	abortErr := model.AbortPreparingTaskSubmission(info.RequestId, model.TaskSubmissionKindMidjourney)
	var pending *model.BillingSettlementApplyPendingError
	if abortErr != nil && !errors.As(abortErr, &pending) {
		info.TaskSubmissionRecoveryProtected = true
		common.SysLog("midjourney rejected submission abort failed: " + abortErr.Error())
		return false
	}
	if pending != nil {
		common.SysLog("midjourney rejected submission cancellation financial application remains pending")
	}
	info.FinalPreConsumedQuota = 0
	return true
}

func failMidjourneyBeforeSend(c *gin.Context, info *relaycommon.RelayInfo, refund bool) {
	if info.TaskSubmissionRecoveryProtected {
		return
	}
	if info.TaskSubmissionRecoveryPrepared {
		abortErr := model.AbortPreparingTaskSubmission(info.RequestId, model.TaskSubmissionKindMidjourney)
		var pending *model.BillingSettlementApplyPendingError
		if abortErr != nil && !errors.As(abortErr, &pending) {
			info.TaskSubmissionRecoveryProtected = true
			common.SysLog("midjourney pre-send abort failed; refund suppressed: " + abortErr.Error())
			return
		}
		if pending != nil {
			common.SysLog("midjourney pre-send cancellation financial application remains pending")
		}
		info.FinalPreConsumedQuota = 0
		return
	}
	if refund {
		if err := service.ReturnPreConsumedQuota(c, info); err != nil {
			common.SysLog("midjourney fallback pre-send refund remains pending: " + err.Error())
		}
	}
}

func midjourneySubmissionStateUnknown(info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	return service.MidjourneyErrorWrapper(constant.MjRequestError,
		"accepted_state_unknown; do not retry; contact an administrator with request_id="+info.RequestId)
}

func RelaySwapFace(c *gin.Context, info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	var swapFaceRequest dto.SwapFaceRequest
	err := common.UnmarshalBodyReusable(c, &swapFaceRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	info.InitChannelMeta(c)

	if swapFaceRequest.SourceBase64 == "" || swapFaceRequest.TargetBase64 == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "sour_base64_and_target_base64_is_required")
	}
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}
	info.PriceData = priceData

	userQuota, err := model.GetUserQuota(info.UserId, false)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	if userQuota-priceData.Quota < 0 {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "quota_not_enough",
		}
	}
	info.Action = constant.MjActionSwapFace
	responseReservation, replayed, recoveryErr := prepareMidjourneySubmissionRecovery(c, info)
	if recoveryErr != nil {
		return recoveryErr
	}
	if replayed {
		return nil
	}
	reservationTransferred := false
	defer func() {
		if !reservationTransferred {
			responseReservation.Release()
		}
	}()
	if apiErr := service.PreConsumeQuota(c, priceData.Quota, info); apiErr != nil {
		failMidjourneyBeforeSend(c, info, false)
		return &dto.MidjourneyResponse{Code: 4, Description: apiErr.Error()}
	}
	responseWriter, err := common.NewBufferedResponseWriterWithReservation(c.Writer, responseReservation)
	if err != nil {
		failMidjourneyBeforeSend(c, info, true)
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "response_buffer_unavailable")
	}
	reservationTransferred = true
	responseCommitted := false
	defer func() {
		if !responseCommitted {
			responseWriter.Discard()
		}
	}()
	requestURL := getMjRequestPath(c.Request.URL.String())
	baseURL := c.GetString("base_url")
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)
	if stateErr := markMidjourneySubmissionUncertain(info, "midjourney-swap-face", priceData.Quota); stateErr != nil {
		failMidjourneyBeforeSend(c, info, true)
		return stateErr
	}
	mjResp, _, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		return midjourneySubmissionStateUnknown(info)
	}
	accepted := mjResp.StatusCode == http.StatusOK && mjResp.Response.Code == 1
	if !accepted {
		explicitRejection := taskHTTPResponseIsExplicitRejection(mjResp.StatusCode) ||
			(mjResp.StatusCode == http.StatusOK && mjResp.Response.Code != 1)
		if !explicitRejection || !rejectMidjourneySubmission(c, info, true) {
			return midjourneySubmissionStateUnknown(info)
		}
	}
	if accepted && strings.TrimSpace(mjResp.Response.Result) == "" {
		return midjourneySubmissionStateUnknown(info)
	}
	chargedQuota := 0
	if accepted {
		chargedQuota = priceData.Quota
	}
	midjResponse := &mjResp.Response
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return midjourneySubmissionStateUnknown(info)
	}
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(mjResp.StatusCode)
	if _, err := responseWriter.Write(respBody); err != nil {
		return midjourneySubmissionStateUnknown(info)
	}
	midjourneyTask := &model.Midjourney{
		UserId:                 info.UserId,
		Code:                   midjResponse.Code,
		Action:                 constant.MjActionSwapFace,
		MjId:                   midjResponse.Result,
		Prompt:                 "InsightFace",
		PromptEn:               "",
		Description:            midjResponse.Description,
		State:                  "",
		SubmitTime:             info.StartTime.UnixNano() / int64(time.Millisecond),
		StartTime:              time.Now().UnixNano() / int64(time.Millisecond),
		FinishTime:             0,
		ImageUrl:               "",
		Status:                 "",
		Progress:               "0%",
		FailReason:             "",
		ChannelId:              c.GetInt("channel_id"),
		Quota:                  chargedQuota,
		TokenId:                info.TokenId,
		Group:                  info.UsingGroup,
		ChargedGroupRatio:      priceData.GroupRatioInfo.GroupRatio,
		BillingSource:          info.BillingSource,
		SubscriptionId:         info.SubscriptionId,
		SubscriptionResetEpoch: info.SubscriptionResetEpoch,
		BillingRequestId:       info.RequestId,
	}
	if !accepted {
		midjourneyTask.BillingSource = service.BillingSourceFree
		midjourneyTask.BillingRequestId = ""
	}
	if accepted {
		publicResponse, snapshotErr := TaskSubmissionPublicResponse(responseWriter)
		if snapshotErr != nil {
			return midjourneySubmissionStateUnknown(info)
		}
		outcome, commitErr := service.CommitMidjourneySubmissionWithRecoveryAndResponse(c, info, midjourneyTask, false, publicResponse)
		if !outcome.Durable {
			return midjourneySubmissionStateUnknown(info)
		}
		if commitErr != nil {
			common.SysLog("midjourney swap-face accepted with pending durable follow-up: " + commitErr.Error())
		}
	} else {
		err = midjourneyTask.Insert()
	}
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "insert_midjourney_task_failed")
	}
	if err := responseWriter.Commit(); err != nil {
		common.SysError("commit buffered midjourney swap-face response failed: " + err.Error())
	}
	responseCommitted = true
	return nil
}

func RelayMidjourneyTaskImageSeed(c *gin.Context) *dto.MidjourneyResponse {
	taskId := c.Param("id")
	userId := c.GetInt("id")
	originTask := model.GetByMJId(userId, taskId)
	if originTask == nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_no_found")
	}
	channel, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
	}
	c.Set("channel_id", originTask.ChannelId)
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))

	requestURL := getMjRequestPath(c.Request.URL.String())
	fullRequestURL := fmt.Sprintf("%s%s", channel.GetBaseURL(), requestURL)
	midjResponseWithStatus, _, err := service.DoMidjourneyHttpRequest(c, time.Second*30, fullRequestURL)
	if err != nil {
		return &midjResponseWithStatus.Response
	}
	midjResponse := &midjResponseWithStatus.Response
	c.Writer.WriteHeader(midjResponseWithStatus.StatusCode)
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	service.IOCopyBytesGracefully(c, nil, respBody)
	return nil
}

func RelayMidjourneyTask(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var err error
	var respBody []byte
	switch relayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch:
		taskId := c.Param("id")
		originTask := model.GetByMJId(userId, taskId)
		if originTask == nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "task_no_found",
			}
		}
		midjourneyTask := coverMidjourneyTaskDto(c, originTask)
		respBody, err = common.Marshal(midjourneyTask)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	case relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		var condition = struct {
			IDs []string `json:"ids"`
		}{}
		err = c.BindJSON(&condition)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "do_request_failed",
			}
		}
		var tasks []dto.MidjourneyDto
		if len(condition.IDs) != 0 {
			originTasks := model.GetByMJIds(userId, condition.IDs)
			for _, originTask := range originTasks {
				midjourneyTask := coverMidjourneyTaskDto(c, originTask)
				tasks = append(tasks, midjourneyTask)
			}
		}
		if tasks == nil {
			tasks = make([]dto.MidjourneyDto, 0)
		}
		respBody, err = common.Marshal(tasks)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")

	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	return nil
}

func RelayMidjourneySubmit(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	consumeQuota := true
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	// 违禁词：MJ 不走主 Relay()，需在此扫描 prompt/content（与 /v1 chat 对齐）。
	if agenthook.ScanUserInput != nil {
		scanReq := &dto.GeneralOpenAIRequest{Prompt: midjRequest.Prompt}
		if midjRequest.Content != "" {
			if scanReq.Prompt == nil || scanReq.Prompt == "" {
				scanReq.Prompt = midjRequest.Content
			} else {
				scanReq.Prompt = fmt.Sprintf("%v\n%s", scanReq.Prompt, midjRequest.Content)
			}
		}
		if e := agenthook.ScanUserInput(c.Request.Context(), int64(c.GetInt("id")), int64(c.GetInt("token_id")), "midjourney", scanReq); e != nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, e.Error())
		}
	}

	relayInfo.InitChannelMeta(c)

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyAction { // midjourney plus，需要从customId中获取任务信息
		mjErr := service.CoverPlusActionToNormalAction(&midjRequest)
		if mjErr != nil {
			return mjErr
		}
		relayInfo.RelayMode = relayconstant.RelayModeMidjourneyChange
	}
	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
		midjRequest.Action = constant.MjActionVideo
	}

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyImagine { //绘画任务，此类任务可重复
		if midjRequest.Prompt == "" {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		midjRequest.Action = constant.MjActionImagine
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyDescribe { //按图生文任务，此类任务可重复
		midjRequest.Action = constant.MjActionDescribe
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyEdits { //编辑任务，此类任务可重复
		midjRequest.Action = constant.MjActionEdits
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyShorten { //缩短任务，此类任务可重复，plus only
		midjRequest.Action = constant.MjActionShorten
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyBlend { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionBlend
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyUpload { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionUpload
	} else if midjRequest.TaskId != "" { //放大、变换任务，此类任务，如果重复且已有结果，远端api会直接返回最终结果
		mjId := ""
		if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyChange {
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			} else if midjRequest.Index == 0 {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
			}
			//action = midjRequest.Action
			mjId = midjRequest.TaskId
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneySimpleChange {
			if midjRequest.Content == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
			}
			params := service.ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
			}
			mjId = params.TaskId
			midjRequest.Action = params.Action
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyModal {
			//if midjRequest.MaskBase64 == "" {
			//	return service.MidjourneyErrorWrapper(constant.MjRequestError, "mask_base64_is_required")
			//}
			mjId = midjRequest.TaskId
			midjRequest.Action = constant.MjActionModal
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
			midjRequest.Action = constant.MjActionVideo
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			}
			mjId = midjRequest.TaskId
		}

		originTask := model.GetByMJId(relayInfo.UserId, mjId)
		if originTask == nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
		} else { //原任务的Status=SUCCESS，则可以做放大UPSCALE、变换VARIATION等动作，此时必须使用原来的请求地址才能正确处理
			if setting.MjActionCheckSuccessEnabled {
				if originTask.Status != "SUCCESS" && relayInfo.RelayMode != relayconstant.RelayModeMidjourneyModal {
					return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_status_not_success")
				}
			}
			channel, err := model.GetChannelById(originTask.ChannelId, true)
			if err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
			}
			if channel.Status != common.ChannelStatusEnabled {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
			}
			c.Set("base_url", channel.GetBaseURL())
			c.Set("channel_id", originTask.ChannelId)
			c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
			logger.LogDebug(c, "Midjourney action uses origin channel id=%s base_url_%s", strconv.Itoa(originTask.ChannelId), logger.PayloadMetadata([]byte(channel.GetBaseURL())))
		}
		midjRequest.Prompt = originTask.Prompt

		//if channelType == common.ChannelTypeMidjourneyPlus {
		//	// plus
		//} else {
		//	// 普通版渠道
		//
		//}
	}

	if midjRequest.Action == constant.MjActionInPaint || midjRequest.Action == constant.MjActionCustomZoom {
		consumeQuota = false
	}

	//baseURL := common.ChannelBaseURLs[channelType]
	requestURL := getMjRequestPath(c.Request.URL.String())

	baseURL := c.GetString("base_url")

	//midjRequest.NotifyHook = "http://127.0.0.1:3000/mj/notify"

	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	priceData, err := helper.ModelPriceHelperPerCall(c, relayInfo)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}
	relayInfo.PriceData = priceData

	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	if consumeQuota && userQuota-priceData.Quota < 0 {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "quota_not_enough",
		}
	}
	relayInfo.Action = midjRequest.Action
	responseReservation, replayed, recoveryErr := prepareMidjourneySubmissionRecovery(c, relayInfo)
	if recoveryErr != nil {
		return recoveryErr
	}
	if replayed {
		return nil
	}
	reservationTransferred := false
	defer func() {
		if !reservationTransferred {
			responseReservation.Release()
		}
	}()
	preConsumed := false
	if consumeQuota {
		relayInfo.DeferBillingCommission = true
		if apiErr := service.PreConsumeQuota(c, priceData.Quota, relayInfo); apiErr != nil {
			failMidjourneyBeforeSend(c, relayInfo, false)
			return &dto.MidjourneyResponse{Code: 4, Description: apiErr.Error()}
		}
		preConsumed = true
	}
	responseWriter, err := common.NewBufferedResponseWriterWithReservation(c.Writer, responseReservation)
	if err != nil {
		failMidjourneyBeforeSend(c, relayInfo, preConsumed)
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "response_buffer_unavailable")
	}
	reservationTransferred = true
	responseCommitted := false
	defer func() {
		if !responseCommitted {
			responseWriter.Discard()
		}
	}()
	if stateErr := markMidjourneySubmissionUncertain(relayInfo, "midjourney", priceData.Quota); stateErr != nil {
		failMidjourneyBeforeSend(c, relayInfo, preConsumed)
		return stateErr
	}
	midjResponseWithStatus, responseBody, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		return midjourneySubmissionStateUnknown(relayInfo)
	}
	midjResponse := &midjResponseWithStatus.Response
	if midjResponseWithStatus.StatusCode != http.StatusOK {
		if taskHTTPResponseIsExplicitRejection(midjResponseWithStatus.StatusCode) && rejectMidjourneySubmission(c, relayInfo, preConsumed) {
			return midjResponse
		}
		return midjourneySubmissionStateUnknown(relayInfo)
	}

	// 文档：https://github.com/novicezk/midjourney-proxy/blob/main/docs/api.md
	//1-提交成功
	// 21-任务已存在（处理中或者有结果了） {"code":21,"description":"任务已存在","result":"0741798445574458","properties":{"status":"SUCCESS","imageUrl":"https://xxxx"}}
	// 22-排队中 {"code":22,"description":"排队中，前面还有1个任务","result":"0741798445574458","properties":{"numberOfQueues":1,"discordInstanceId":"1118138338562560102"}}
	// 23-队列已满，请稍后再试 {"code":23,"description":"队列已满，请稍后尝试","result":"14001929738841620","properties":{"discordInstanceId":"1118138338562560102"}}
	// 24-prompt包含敏感词 {"code":24,"description":"可能包含敏感词","properties":{"promptEn":"nude body","bannedWord":"nude"}}
	// other: 提交错误，description为错误描述
	acceptedCode := midjResponse.Code == 1 || midjResponse.Code == 21 || midjResponse.Code == 22
	if !acceptedCode {
		if !rejectMidjourneySubmission(c, relayInfo, preConsumed) {
			return midjourneySubmissionStateUnknown(relayInfo)
		}
		preConsumed = false
	}
	if acceptedCode && strings.TrimSpace(midjResponse.Result) == "" {
		return midjourneySubmissionStateUnknown(relayInfo)
	}
	midjourneyQuota := 0
	if preConsumed && acceptedCode {
		midjourneyQuota = priceData.Quota
	}
	midjourneyTask := &model.Midjourney{
		UserId:                 relayInfo.UserId,
		Code:                   midjResponse.Code,
		Action:                 midjRequest.Action,
		MjId:                   midjResponse.Result,
		Prompt:                 midjRequest.Prompt,
		PromptEn:               "",
		Description:            midjResponse.Description,
		State:                  "",
		SubmitTime:             time.Now().UnixNano() / int64(time.Millisecond),
		StartTime:              0,
		FinishTime:             0,
		ImageUrl:               "",
		Status:                 "",
		Progress:               "0%",
		FailReason:             "",
		ChannelId:              c.GetInt("channel_id"),
		Quota:                  midjourneyQuota,
		TokenId:                relayInfo.TokenId,
		Group:                  relayInfo.UsingGroup,
		ChargedGroupRatio:      priceData.GroupRatioInfo.GroupRatio,
		BillingSource:          relayInfo.BillingSource,
		SubscriptionId:         relayInfo.SubscriptionId,
		SubscriptionResetEpoch: relayInfo.SubscriptionResetEpoch,
		BillingRequestId:       relayInfo.RequestId,
	}
	if midjResponse.Code == 3 {
		//无实例账号自动禁用渠道（No available account instance）
		channel, err := model.GetChannelById(midjourneyTask.ChannelId, true)
		if err != nil {
			common.SysLog("get_channel_null: " + err.Error())
		}
		if channel.GetAutoBan() && common.AutomaticDisableChannelEnabled {
			model.UpdateChannelStatus(midjourneyTask.ChannelId, "", 2, "No available account instance")
		}
	}
	if midjResponse.Code != 1 && midjResponse.Code != 21 && midjResponse.Code != 22 {
		//非1-提交成功,21-任务已存在和22-排队中，则记录错误原因
		midjourneyTask.FailReason = midjResponse.Description
		consumeQuota = false
		midjourneyTask.Quota = 0
		midjourneyTask.BillingSource = service.BillingSourceFree
		midjourneyTask.BillingRequestId = ""
	}

	if midjResponse.Code == 21 { //21-任务已存在（处理中或者有结果了）
		// 将 properties 转换为一个 map
		properties, ok := midjResponse.Properties.(map[string]interface{})
		if ok {
			imageUrl, ok1 := properties["imageUrl"].(string)
			status, ok2 := properties["status"].(string)
			if ok1 && ok2 {
				midjourneyTask.ImageUrl = imageUrl
				midjourneyTask.Status = status
				if status == "SUCCESS" {
					midjourneyTask.Progress = "100%"
					midjourneyTask.StartTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjourneyTask.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjResponse.Code = 1
				}
			}
		}
		//修改返回值
		if midjRequest.Action != constant.MjActionInPaint && midjRequest.Action != constant.MjActionCustomZoom {
			newBody := strings.Replace(string(responseBody), `"code":21`, `"code":1`, -1)
			responseBody = []byte(newBody)
		}
	}
	if midjResponse.Code == 1 && midjRequest.Action == "UPLOAD" {
		midjourneyTask.Progress = "100%"
		midjourneyTask.Status = "SUCCESS"
	}
	if midjResponse.Code == 22 {
		responseBody = []byte(strings.Replace(string(responseBody), `"code":22`, `"code":1`, -1))
	}
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(midjResponseWithStatus.StatusCode)
	if _, err := responseWriter.Write(responseBody); err != nil {
		return midjourneySubmissionStateUnknown(relayInfo)
	}
	if acceptedCode {
		deferCommission := midjourneyTask.Progress != "100%" || midjourneyTask.Status != "SUCCESS"
		publicResponse, snapshotErr := TaskSubmissionPublicResponse(responseWriter)
		if snapshotErr != nil {
			return midjourneySubmissionStateUnknown(relayInfo)
		}
		outcome, commitErr := service.CommitMidjourneySubmissionWithRecoveryAndResponse(c, relayInfo, midjourneyTask, deferCommission, publicResponse)
		if !outcome.Durable {
			return midjourneySubmissionStateUnknown(relayInfo)
		}
		if commitErr != nil {
			common.SysLog("midjourney task accepted with pending durable follow-up: " + commitErr.Error())
		}
	} else {
		err = midjourneyTask.Insert()
	}
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "insert_midjourney_task_failed",
		}
	}

	if err := responseWriter.Commit(); err != nil {
		common.SysError("commit buffered midjourney response failed: " + err.Error())
	}
	responseCommitted = true
	return nil
}

type taskChangeParams struct {
	ID     string
	Action string
	Index  int
}

func getMjRequestPath(path string) string {
	requestURL := path
	if strings.Contains(requestURL, "/mj-") {
		urls := strings.Split(requestURL, "/mj/")
		if len(urls) < 2 {
			return requestURL
		}
		requestURL = "/mj/" + urls[1]
	}
	return requestURL
}
