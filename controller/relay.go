package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	agenthook "github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {
	c.Header("Cache-Control", "no-store, private")

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError    *types.NewAPIError
		ws             *websocket.Conn
		deferredWriter *common.BufferedResponseWriter
		relayInfo      *relaycommon.RelayInfo
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if panicValue := recover(); panicValue != nil {
			if handleRelayPanic(c, relayInfo, deferredWriter, ws) {
				return
			}
			panic(panicValue)
		}
		finishRelayResponse(c, relayFormat, ws, requestId, deferredWriter, newAPIError)
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}
	bufferNonStreamingResponse := relayFormat != types.RelayFormatOpenAIRealtime && !request.IsStream(c)

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting is disabled.
	var meta *types.TokenCountMeta
	if needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	// 多租户调用前风控（7c · §2.13）：转发前 RPM 限流（+ 租户状态）。命中拦截（429 限流 / 403 IP·状态）。nil = 未装配。
	if agenthook.CheckCall != nil {
		if e := agenthook.CheckCall(c, int64(c.GetInt("id")), int64(c.GetInt("token_id")), relayInfo.OriginModelName, c.ClientIP(), ""); e != nil {
			newAPIError = e
			return
		}
	}

	// 多租户违禁词审核（6e · §2.14）：转发前扫用户输入；命中 block 拦截、remind 仅记录。nil = 未装配。
	if agenthook.ScanUserInput != nil {
		if e := agenthook.ScanUserInput(c, int64(c.GetInt("id")), int64(c.GetInt("token_id")), relayInfo.OriginModelName, request); e != nil {
			newAPIError = e
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = finalizeRelayBillingFailure(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		relayInfo.InitChannelMeta(c)
		if capabilityErr := relay.ValidateMediaStreamingCapability(relayInfo); capabilityErr != nil {
			newAPIError = capabilityErr
			break
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)
		newAPIError, bodyErr = invokeRelayAttemptAfterAdmission(c, bufferNonStreamingResponse, &deferredWriter, func() *types.NewAPIError {
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				return relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				return relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				return geminiRelayHandler(c, relayInfo)
			default:
				return relayHandler(c, relayInfo)
			}
		})
		if bodyErr != nil {
			newAPIError = relayResponseAdmissionError(bodyErr)
			break
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		discardRelayAttemptResponse(c, deferredWriter)
		deferredWriter = nil

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func relayResponseAdmissionError(err error) *types.NewAPIError {
	common.SysError(fmt.Sprintf("relay response admission failed before provider call: error_type=%T", err))
	return types.NewErrorWithStatusCode(
		errors.New("response capacity is temporarily unavailable; retry later"),
		types.ErrorCodeDoRequestFailed,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

var finalizeAcceptedBillingFailure = service.FinalizeAcceptedBillingFailure

// invokeRelayAttemptAfterAdmission exposes the admitted writer to the caller
// before invoking any provider code. The caller's panic cleanup can therefore
// always discard the private spool and release its reservation.
func invokeRelayAttemptAfterAdmission(c *gin.Context, enabled bool, writer **common.BufferedResponseWriter, invoke func() *types.NewAPIError) (*types.NewAPIError, error) {
	admitted, err := beginRelayAttemptResponse(c, enabled)
	if err != nil {
		return nil, err
	}
	if writer != nil {
		*writer = admitted
	}
	return invoke(), nil
}

func beginRelayAttemptResponse(c *gin.Context, enabled bool) (*common.BufferedResponseWriter, error) {
	if !enabled || c == nil || c.Writer == nil {
		return nil, nil
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	writer, err := common.NewBufferedResponseWriterContext(ctx, c.Writer)
	if err != nil {
		return nil, err
	}
	c.Writer = writer
	return writer, nil
}

func discardRelayAttemptResponse(c *gin.Context, writer *common.BufferedResponseWriter) {
	if writer == nil {
		return
	}
	writer.Discard()
	if c != nil {
		c.Writer = writer.Underlying()
	}
}

func compensateRelayPanic(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil || relayInfo.Billing == nil || service.IsUpstreamAccepted(c) || !relayInfo.Billing.NeedsRefund() {
		return
	}
	if err := relayInfo.Billing.Refund(c); err != nil {
		common.SysError(fmt.Sprintf("billing refund after relay panic failed: error_type=%T", err))
	}
}

func cleanupRelayPanic(c *gin.Context, relayInfo *relaycommon.RelayInfo, writer *common.BufferedResponseWriter) {
	if writer != nil {
		defer func() {
			writer.Discard()
			if c != nil {
				c.Writer = writer.Underlying()
			}
		}()
	}
	if !service.IsUpstreamAccepted(c) {
		compensateRelayPanic(c, relayInfo)
		return
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			common.SysError(fmt.Sprintf("accepted relay panic billing finalization panicked: panic_type=%T", panicValue))
		}
	}()
	apiErr := types.NewErrorWithStatusCode(
		errors.New("upstream accepted; response unavailable; do not retry/contact admin"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
	if terminalErr := finalizeAcceptedBillingFailure(c, relayInfo, apiErr); terminalErr != nil && !service.IsBillingTerminalAttempted(c) {
		common.SysError(fmt.Sprintf("accepted relay panic billing terminal state remains unavailable: error_code=%s", terminalErr.GetErrorCode()))
	}
}

func handleRelayPanic(c *gin.Context, relayInfo *relaycommon.RelayInfo, writer *common.BufferedResponseWriter, ws *websocket.Conn) bool {
	suppressRepanic := service.IsUpstreamAccepted(c) && writer == nil && (ws != nil || c != nil && c.Writer != nil && c.Writer.Written())
	cleanupRelayPanic(c, relayInfo, writer)
	if !suppressRepanic {
		return false
	}
	common.SysLog("accepted streaming relay panicked after response publication; connection closed without appending an error payload")
	if c != nil {
		c.Abort()
	}
	return true
}

func finalizeRelayBillingFailure(c *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if relayInfo != nil && !service.IsUpstreamAccepted(c) {
		if relayInfo.Billing != nil {
			if err := relayInfo.Billing.Refund(c); err != nil {
				common.SysError(fmt.Sprintf("billing refund failed: error_type=%T", err))
			}
		}
	} else if relayInfo != nil {
		apiErr = finalizeAcceptedBillingFailure(c, relayInfo, apiErr)
	}
	service.ChargeViolationFeeIfNeeded(c, relayInfo, apiErr)
	return apiErr
}

func finishRelayResponse(c *gin.Context, relayFormat types.RelayFormat, ws *websocket.Conn, requestId string, deferredWriter *common.BufferedResponseWriter, apiErr *types.NewAPIError) {
	if apiErr == nil {
		if deferredWriter == nil {
			return
		}
		c.Writer = deferredWriter.Underlying()
		if err := deferredWriter.Commit(); err != nil {
			common.SysLog("critical: failed to flush accepted relay response after durable billing: " + err.Error())
		}
		return
	}

	logger.LogError(c, fmt.Sprintf("relay error status=%d error_%s", apiErr.StatusCode, logger.PayloadMetadata([]byte(apiErr.Error()))))
	if deferredWriter != nil {
		deferredWriter.Discard()
		c.Writer = deferredWriter.Underlying()
	}
	apiErr.SetMessage(common.MessageWithRequestId(apiErr.Error(), requestId))
	if deferredWriter == nil && service.IsUpstreamAccepted(c) && c.Writer != nil && c.Writer.Written() {
		common.SysLog("critical: post-upstream billing failed after a streaming response was written; closing without appending an invalid error payload")
		return
	}
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		helper.WssError(c, ws, apiErr.ToOpenAIError())
	case types.RelayFormatClaude:
		c.JSON(apiErr.StatusCode, gin.H{
			"type":  "error",
			"error": apiErr.ToClaudeError(),
		})
	default:
		c.JSON(apiErr.StatusCode, gin.H{
			"error": apiErr.ToOpenAIError(),
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin:  middleware.IsWebSocketOriginAllowed,
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = common.SaturatingUintToInt(maxCompletionTokens)
		} else {
			meta.MaxTokens = common.SaturatingUintToInt(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = common.SaturatingUintToInt(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = common.SaturatingUintToInt(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.IsUpstreamAccepted(c) {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error channel_id=%d status_code=%d error_%s", channelError.ChannelId, err.StatusCode, logger.PayloadMetadata([]byte(err.Error()))))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		errorMetadata := logger.PayloadMetadata([]byte(mjErr.Description + "\x00" + mjErr.Result))
		logger.LogError(c, fmt.Sprintf("relay error channel_id=%d status_code=%d code=%d error_%s", channelId, statusCode, mjErr.Code, errorMetadata))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		panicValue := recover()
		if panicValue != nil && result != nil && result.ClientResponse != nil {
			result.ClientResponse.Discard()
		}
		if (taskErr != nil || panicValue != nil) && !relayInfo.TaskSubmissionRecoveryProtected {
			cleanupFailedTaskSubmission(c, relayInfo)
		}
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}
		if !taskSubmissionRetryAllowed(relayInfo) {
			break
		}
		if errors.Is(taskErr.Error, common.ErrBufferedResponseCapacity) {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil && result != nil && !result.Replayed {
		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.SubscriptionResetEpoch = relayInfo.SubscriptionResetEpoch
		task.PrivateData.SubscriptionOccurredAt = relayInfo.SubscriptionOccurredAt
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios,
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		taskErr = finalizeTaskSubmissionResponse(c, relayInfo, result, task, service.CommitTaskSubmissionWithRecoveryAndResponse)
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func taskSubmissionRetryAllowed(relayInfo *relaycommon.RelayInfo) bool {
	return relayInfo == nil || !relayInfo.TaskSubmissionRecoveryProtected
}

func cleanupFailedTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	cleanupFailedTaskSubmissionWithAbort(c, relayInfo, model.AbortPreparingTaskSubmission)
}

func cleanupFailedTaskSubmissionWithAbort(c *gin.Context, relayInfo *relaycommon.RelayInfo, abort func(string, string) error) {
	if relayInfo == nil || relayInfo.TaskSubmissionRecoveryProtected {
		return
	}
	if relayInfo.TaskSubmissionRecoveryPrepared {
		abortErr := abort(relayInfo.RequestId, relayInfo.TaskSubmissionRecoveryKind)
		var pending *model.BillingSettlementApplyPendingError
		if abortErr != nil && !errors.As(abortErr, &pending) {
			relayInfo.TaskSubmissionRecoveryProtected = true
			common.SysError(fmt.Sprintf("task submission abort failed; refund suppressed: error_type=%T", abortErr))
			return
		}
		if pending != nil {
			common.SysError("task submission cancellation committed with pending financial application")
		}
		return
	}
	if relayInfo.Billing != nil {
		if refundErr := relayInfo.Billing.Refund(c); refundErr != nil {
			common.SysError(fmt.Sprintf("task billing fallback refund failed: error_type=%T", refundErr))
		}
	}
}

type taskSubmissionFinalizer func(*gin.Context, *relaycommon.RelayInfo, *model.Task, int, *model.TaskSubmissionPublicResponse) (service.TaskSubmissionCommitOutcome, error)

func finalizeTaskSubmissionResponse(c *gin.Context, relayInfo *relaycommon.RelayInfo, result *relay.TaskSubmitResult, task *model.Task, finalize taskSubmissionFinalizer) *dto.TaskError {
	if result == nil || result.ClientResponse == nil || finalize == nil {
		if result != nil && result.ClientResponse != nil {
			result.ClientResponse.Discard()
		}
		if relayInfo != nil {
			relayInfo.TaskSubmissionRecoveryProtected = true
		}
		return service.TaskErrorWrapperLocal(errors.New("task submission durability finalizer is unavailable"), "accepted_state_unknown", http.StatusInternalServerError)
	}
	publicResponse, snapshotErr := relay.TaskSubmissionPublicResponse(result.ClientResponse)
	if snapshotErr != nil {
		result.ClientResponse.Discard()
		if relayInfo != nil {
			relayInfo.TaskSubmissionRecoveryProtected = true
		}
		return &dto.TaskError{
			Code: "accepted_state_unknown", Message: fmt.Sprintf("upstream accepted the task but its replayable response could not be frozen; do not retry; contact an administrator with request_id=%s", relayInfo.RequestId),
			StatusCode: http.StatusInternalServerError, LocalError: true, Error: snapshotErr,
		}
	}
	outcome, err := finalize(c, relayInfo, task, result.Quota, publicResponse)
	if outcome.Durable {
		relayInfo.TaskSubmissionRecoveryProtected = true
		if err != nil {
			common.SysError("accepted task submission has a pending durable follow-up: " + err.Error())
		}
		if commitErr := result.ClientResponse.Commit(); commitErr != nil {
			common.SysError("commit buffered task response failed: " + commitErr.Error())
		}
		return nil
	}

	result.ClientResponse.Discard()
	relayInfo.TaskSubmissionRecoveryProtected = true
	if err == nil {
		err = errors.New("task submission ACK could not be frozen")
	}
	return &dto.TaskError{
		Code:       "accepted_state_unknown",
		Message:    fmt.Sprintf("upstream accepted the task but local acceptance state is unknown; do not retry; contact an administrator with request_id=%s", relayInfo.RequestId),
		StatusCode: http.StatusInternalServerError,
		LocalError: true,
		Error:      err,
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
