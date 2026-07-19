package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func projectionLogRequestId(c *gin.Context, settlementRequestId string, projectionKey string) string {
	if c != nil {
		if requestId := strings.TrimSpace(c.GetString(common.RequestIdKey)); requestId != "" && len(requestId) <= 64 {
			return requestId
		}
	}
	if requestId := strings.TrimSpace(settlementRequestId); requestId != "" && len(requestId) <= 64 {
		return requestId
	}
	return projectionKey
}

func projectionClientIP(c *gin.Context, userId int) string {
	if c == nil {
		return ""
	}
	settings, err := model.GetUserSetting(userId, false)
	if err != nil || !settings.RecordIpLog {
		return ""
	}
	return c.ClientIP()
}

func taskInitialBillingProjection(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) *model.BillingProjectionSpec {
	if info == nil || task == nil {
		return nil
	}
	projectionKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "task_initial")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent += "，按次计费"
	} else {
		keys := make([]string, 0, len(info.PriceData.OtherRatios))
		for key, ratio := range info.PriceData.OtherRatios {
			if ratio != 1 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s: %.2f", key, info.PriceData.OtherRatios[key]))
		}
		if len(parts) > 0 {
			logContent += ", 计算参数：" + strings.Join(parts, ", ")
		}
	}
	other := map[string]interface{}{
		"is_task": true, "model_price": info.PriceData.ModelPrice,
		"group_ratio": info.PriceData.GroupRatioInfo.GroupRatio,
	}
	if c != nil && c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	appendBillingInfo(info, other)
	createdAt := task.SubmitTime
	if createdAt <= 0 {
		createdAt = common.GetTimestamp()
	}
	username, tokenName, upstreamRequestId := "", "", ""
	if c != nil {
		username = c.GetString("username")
		tokenName = c.GetString("token_name")
		upstreamRequestId = c.GetString(common.UpstreamRequestIdKey)
	}
	quota := info.PriceData.Quota
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyRequestId: info.RequestId, DependencyOperation: billingSettlementOperation,
		LogEnabled: common.LogConsumeEnabled, LogUserId: info.UserId, LogUsername: username, LogCreatedAt: createdAt,
		LogType: model.LogTypeConsume, LogContent: logContent, LogTokenName: tokenName, LogModelName: info.OriginModelName,
		LogQuota: quota, LogChannelId: info.ChannelId, LogTokenId: info.TokenId, LogGroup: info.UsingGroup,
		LogIp: projectionClientIP(c, info.UserId), LogRequestId: projectionLogRequestId(c, info.RequestId, projectionKey),
		LogUpstreamRequestId: upstreamRequestId, LogOther: common.MapToJsonStr(other),
		QuotaDataEnabled: common.DataExportEnabled && common.LogConsumeEnabled, QuotaDataNodeName: common.NodeName,
		UserId: info.UserId, UserUsedQuotaDelta: quota, UserRequestDelta: 1,
		ChannelId: info.ChannelId, ChannelQuotaDelta: quota,
	}
}

func taskAdjustmentBillingProjection(task *model.Task, preConsumedQuota int, actualQuota int, reason string) *model.BillingProjectionSpec {
	balanceDelta := preConsumedQuota - actualQuota
	if task == nil || balanceDelta == 0 {
		return nil
	}
	requestId := taskBillingRequestId(task)
	projectionKey := model.BillingProjectionKey(requestId, billingSettlementOperation, "task_terminal_adjustment")
	logType := model.LogTypeRefund
	logQuota := balanceDelta
	userQuotaDelta, channelQuotaDelta := 0, 0
	if balanceDelta < 0 {
		logType = model.LogTypeConsume
		logQuota = -balanceDelta
		userQuotaDelta = logQuota
		channelQuotaDelta = logQuota
	}
	username, _ := model.GetUsernameById(task.UserId, false)
	tokenName := ""
	if task.PrivateData.TokenId > 0 {
		if token, err := model.GetTokenById(task.PrivateData.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	createdAt := task.FinishTime
	if createdAt <= 0 {
		createdAt = common.GetTimestamp()
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyRequestId: requestId, DependencyOperation: billingSettlementOperation,
		LogEnabled: logType != model.LogTypeConsume || common.LogConsumeEnabled,
		LogUserId:  task.UserId, LogUsername: username, LogCreatedAt: createdAt, LogType: logType, LogContent: reason,
		LogTokenName: tokenName, LogModelName: taskModelName(task), LogQuota: logQuota, LogChannelId: task.ChannelId,
		LogTokenId: task.PrivateData.TokenId, LogGroup: task.Group, LogRequestId: projectionLogRequestId(nil, requestId, projectionKey),
		LogOther: common.MapToJsonStr(other), QuotaDataEnabled: logType == model.LogTypeConsume && common.DataExportEnabled && common.LogConsumeEnabled,
		QuotaDataNodeName: nodeName, UserId: task.UserId, UserUsedQuotaDelta: userQuotaDelta,
		UserRequestDelta: 0, ChannelId: task.ChannelId, ChannelQuotaDelta: channelQuotaDelta,
	}
}

func midjourneyInitialBillingProjection(c *gin.Context, info *relaycommon.RelayInfo, task *model.Midjourney) *model.BillingProjectionSpec {
	if info == nil || task == nil {
		return nil
	}
	projectionKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "midjourney_initial")
	content := fmt.Sprintf("模型固定价格 %.2f，分组倍率 %.2f，操作 %s", info.PriceData.ModelPrice, info.PriceData.GroupRatioInfo.GroupRatio, task.Action)
	if task.Action != constant.MjActionSwapFace {
		content += "，ID " + task.MjId
	}
	username, tokenName, upstreamRequestId := "", "", ""
	if c != nil {
		username = c.GetString("username")
		tokenName = c.GetString("token_name")
		upstreamRequestId = c.GetString(common.UpstreamRequestIdKey)
	}
	createdAt := task.SubmitTime / 1000
	if createdAt <= 0 {
		createdAt = common.GetTimestamp()
	}
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyRequestId: info.RequestId, DependencyOperation: billingSettlementOperation,
		LogEnabled: common.LogConsumeEnabled, LogUserId: info.UserId, LogUsername: username, LogCreatedAt: createdAt,
		LogType: model.LogTypeConsume, LogContent: content, LogTokenName: tokenName,
		LogModelName: CovertMjpActionToModelName(task.Action), LogQuota: task.Quota, LogChannelId: info.ChannelId,
		LogTokenId: info.TokenId, LogGroup: info.UsingGroup, LogIp: projectionClientIP(c, info.UserId),
		LogRequestId: projectionLogRequestId(c, info.RequestId, projectionKey), LogUpstreamRequestId: upstreamRequestId,
		LogOther:         common.MapToJsonStr(GenerateMjOtherInfo(info, info.PriceData)),
		QuotaDataEnabled: common.DataExportEnabled && common.LogConsumeEnabled, QuotaDataNodeName: common.NodeName,
		UserId: info.UserId, UserUsedQuotaDelta: task.Quota, UserRequestDelta: 1,
		ChannelId: info.ChannelId, ChannelQuotaDelta: task.Quota,
	}
}

func midjourneyTerminalBillingProjection(task *model.Midjourney, preConsumedQuota int, reason string) *model.BillingProjectionSpec {
	if task == nil || preConsumedQuota <= 0 {
		return nil
	}
	requestId := strings.TrimSpace(task.BillingRequestId)
	if requestId == "" {
		requestId = fmt.Sprintf("midjourney:%d", task.Id)
	}
	projectionKey := model.BillingProjectionKey(requestId, billingSettlementOperation, "midjourney_terminal_refund")
	username, _ := model.GetUsernameById(task.UserId, false)
	tokenName := ""
	if task.TokenId > 0 {
		if token, err := model.GetTokenById(task.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := task.FinishTime / 1000
	if createdAt <= 0 {
		createdAt = common.GetTimestamp()
	}
	other := map[string]interface{}{"task_id": task.MjId, "reason": reason}
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyRequestId: requestId, DependencyOperation: billingSettlementOperation,
		LogEnabled: true, LogUserId: task.UserId, LogUsername: username, LogCreatedAt: createdAt,
		LogType: model.LogTypeRefund, LogContent: reason, LogTokenName: tokenName,
		LogModelName: CovertMjpActionToModelName(task.Action), LogQuota: preConsumedQuota,
		LogChannelId: task.ChannelId, LogTokenId: task.TokenId, LogGroup: task.Group,
		LogRequestId: projectionLogRequestId(nil, requestId, projectionKey), LogOther: common.MapToJsonStr(other),
	}
}
