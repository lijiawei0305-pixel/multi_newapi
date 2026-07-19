package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskBillingAdjustmentCopiesSubscriptionPeriodBoundary(t *testing.T) {
	task := &model.Task{
		ID: 42, Quota: 100, SubmitTime: 700,
		PrivateData: model.TaskPrivateData{
			BillingSource: BillingSourceSubscription, SubscriptionId: 91,
			SubscriptionResetEpoch: 500, SubscriptionOccurredAt: 600, TokenId: 92,
		},
	}
	spec, balanceDelta := taskBillingAdjustment(task, 80)
	require.NotNil(t, spec)
	assert.Equal(t, 20, balanceDelta)
	assert.Equal(t, 91, spec.SubscriptionId)
	assert.Equal(t, int64(500), spec.SubscriptionResetEpoch)
	assert.Equal(t, int64(600), spec.SubscriptionOccurredAt)
	assert.Equal(t, int64(-20), spec.SubscriptionQuotaDelta)
	assert.Equal(t, 20, spec.TokenQuotaDelta)

	task.PrivateData.SubscriptionOccurredAt = 0
	spec, _ = taskBillingAdjustment(task, 80)
	require.NotNil(t, spec)
	assert.Equal(t, task.SubmitTime, spec.SubscriptionOccurredAt, "legacy task rows use their durable submit time")
}

func TestDeferredTaskUsesFrozenCommissionPolicyAfterConfigurationChange(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 89, 89, 89
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "task-policy-token", 1000)
	seedChannel(t, channelID)
	prepareCalls := 0
	frozenTime := time.Now().UTC().Truncate(time.Millisecond)
	previousPrepare := agenthook.PrepareConsumeCommissionPolicy
	previousMaterialize := agenthook.MaterializeConsumeCommissionPolicy
	agenthook.PrepareConsumeCommissionPolicy = func(int64, string, string, float64) (agenthook.CommissionPolicy, error) {
		prepareCalls++
		if prepareCalls > 1 {
			return agenthook.CommissionPolicy{}, errors.New("policy was re-read after upstream work")
		}
		return agenthook.CommissionPolicy{
			OccurredAt: frozenTime, WalletTenantID: 7, WalletUserID: userID,
			EarningMode: "direct", EarningTenantID: 7, EarningUserID: 999,
			EarningSourceType: "consume_commission", EarningRemark: "consume:wallet", DirectRate: 0.01,
		}, nil
	}
	agenthook.MaterializeConsumeCommissionPolicy = func(policy agenthook.CommissionPolicy, quota int64, sourceID string) (agenthook.CommissionSnapshot, error) {
		return agenthook.CommissionSnapshot{
			SourceID: sourceID, OccurredAt: policy.OccurredAt,
			WalletTenantID: policy.WalletTenantID, WalletUserID: policy.WalletUserID, WalletQuota: quota,
			EarningApplicable: true, EarningTenantID: policy.EarningTenantID, EarningUserID: policy.EarningUserID,
			EarningSourceType: policy.EarningSourceType, EarningAmount: float64(quota) * policy.DirectRate, EarningRemark: policy.EarningRemark,
		}, nil
	}
	t.Cleanup(func() {
		agenthook.PrepareConsumeCommissionPolicy = previousPrepare
		agenthook.MaterializeConsumeCommissionPolicy = previousMaterialize
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "task-policy-token", RequestId: "task-frozen-policy",
		OriginModelName: "test-model", UsingGroup: "default", TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	require.Nil(t, PreConsumeBilling(c, 100, info))
	task := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	require.NoError(t, CommitTaskSubmission(info, task, 80))
	fromStatus := task.Status
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	won, err := TransitionTaskWithBilling(context.Background(), task, fromStatus, 80, "success")
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, 1, prepareCalls)
	var event model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&event).Error)
	assert.Equal(t, int64(999), event.CommissionEarningUserId)
	assert.Equal(t, 0.8, event.CommissionEarningAmount)
	assert.Equal(t, frozenTime, *event.CommissionOccurredAt)
}

func TestMidjourneyHardInsertFailureLeavesReservedRootThenCancels(t *testing.T) {
	truncate(t)
	const userID, tokenID = 90, 90
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "mj-hard-failure-token", 1000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "mj-hard-failure-token", RequestId: "mj-hard-insert",
		UsingGroup: "default", OriginModelName: "mj_imagine", DeferBillingCommission: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1},
	}
	require.Nil(t, PreConsumeQuota(c, 60, info))
	task := &model.Midjourney{
		UserId: userID, MjId: "upstream-mj-hard", Status: "SUBMITTED", Progress: "0%", Quota: 60,
		TokenId: tokenID, Group: "default", BillingSource: BillingSourceWallet, BillingRequestId: info.RequestId,
	}
	const callbackName = "test:fail_midjourney_insert"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Midjourney" {
			tx.AddError(errors.New("injected midjourney insert failure"))
		}
	}))
	require.ErrorContains(t, CommitMidjourneySubmission(info, task, true), "injected midjourney insert failure")
	assert.Zero(t, task.Id)
	require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	var event model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&event).Error)
	assert.Equal(t, model.BillingSettlementStatusReserved, event.Status)
	assert.Equal(t, 940, getUserQuota(t, userID))
	require.NoError(t, ReturnPreConsumedQuota(c, info))
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&event).Error)
	assert.Equal(t, model.BillingSettlementStatusCancelled, event.Status)
	assert.Equal(t, 1000, getUserQuota(t, userID))
	assert.Equal(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestTaskInitialProjectionCrashReplaysAuthoritativeQuotaExactlyOnce(t *testing.T) {
	truncate(t)
	oldDataExport, oldNodeName := common.DataExportEnabled, common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "task-origin-node"
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExport
		common.NodeName = oldNodeName
	})
	const userID, tokenID, channelID = 91, 91, 91
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "task-projection-token", 1000)
	seedChannel(t, channelID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("username", "projection-user")
	c.Set("token_name", "projection-token")
	c.Set(common.RequestIdKey, "task-projection-log-request")
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "task-projection-token", RequestId: "task-initial-projection",
		OriginModelName: "task-projection-model", UsingGroup: "default",
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	info.PriceData.Quota = 99 // Deliberately differs from the accepted upstream result.
	info.PriceData.ModelPrice = 0.01
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeBilling(c, 100, info))
	task := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	task.SubmitTime = 7201

	const callbackName = "test:fail_task_initial_projection_log"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Log" {
			tx.AddError(errors.New("injected initial projection log failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Create().Remove(callbackName)
		}
	})
	require.NoError(t, CommitTaskSubmissionWithProjection(c, info, task, 80), "projection sink failure must not lose the accepted task")
	assert.NotZero(t, task.ID)
	require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	callbackRegistered = false

	projectionKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "task_initial")
	var outbox model.BillingProjectionOutbox
	require.NoError(t, model.DB.Where("projection_key = ?", projectionKey).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusPending, outbox.Status)
	assert.Equal(t, 80, outbox.LogQuota)
	assert.Equal(t, 80, outbox.UserUsedQuotaDelta)
	assert.Equal(t, 80, outbox.ChannelQuotaDelta)
	assert.True(t, outbox.QuotaDataEnabled)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	assert.Zero(t, countLogs(t))
	var quotaDataCount int64
	require.NoError(t, model.DB.Model(&model.QuotaData{}).Count(&quotaDataCount).Error)
	assert.Zero(t, quotaDataCount)

	applied, err := model.ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	applied, err = model.ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Zero(t, applied)
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 80, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.Equal(t, int64(80), channel.UsedQuota)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 80, log.Quota)
	assert.NotNil(t, log.ProjectionKey)
	assert.Equal(t, projectionKey, *log.ProjectionKey)
	var quotaData model.QuotaData
	require.NoError(t, model.DB.First(&quotaData).Error)
	assert.Equal(t, 1, quotaData.Count)
	assert.Equal(t, 80, quotaData.Quota)
	assert.Equal(t, "task-origin-node", quotaData.NodeName)
}

func TestMidjourneyInitialAndTerminalProjectionsAreIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 92, 92, 92
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "mj-projection-token", 1000)
	seedChannel(t, channelID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil)
	c.Set("username", "projection-user")
	c.Set("token_name", "projection-token")
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "mj-projection-token", RequestId: "mj-projection",
		OriginModelName: "mj_imagine", UsingGroup: "default", DeferBillingCommission: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	info.PriceData.Quota = 60
	info.PriceData.ModelPrice = 0.03
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeQuota(c, 60, info))
	task := &model.Midjourney{
		UserId: userID, MjId: "mj-projection-upstream", Action: "IMAGINE", Status: "SUBMITTED", Progress: "0%",
		SubmitTime: 7_201_000, Quota: 60, ChannelId: channelID, TokenId: tokenID, Group: "default",
		BillingSource: BillingSourceWallet, BillingRequestId: info.RequestId,
	}
	require.NoError(t, CommitMidjourneySubmissionWithProjection(c, info, task, true))
	assert.NotZero(t, task.Id)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 60, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)

	fromStatus := task.Status
	task.Status = "FAILURE"
	task.Progress = "100%"
	task.FinishTime = 8_001_000
	task.FailReason = "terminal upstream failure"
	won, err := TransitionMidjourneyWithBilling(context.Background(), task, fromStatus, task.FailReason)
	require.NoError(t, err)
	assert.True(t, won)
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 60, user.UsedQuota, "refund logs do not subtract historical usage")
	assert.Equal(t, 1, user.RequestCount, "terminal adjustment must not double-count the request")
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.Equal(t, int64(60), channel.UsedQuota)
	assert.Equal(t, int64(2), countLogs(t))

	initialKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "midjourney_initial")
	terminalKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "midjourney_terminal_refund")
	require.NoError(t, model.ApplyBillingProjection(initialKey))
	require.NoError(t, model.ApplyBillingProjection(terminalKey))
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 60, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, int64(2), countLogs(t))
}

func TestTaskTerminalAdditionalChargeDoesNotDoubleCountRequest(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 93, 93, 93
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "task-count-token", 1000)
	seedChannel(t, channelID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("username", "count-user")
	c.Set("token_name", "count-token")
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "task-count-token", RequestId: "task-request-count",
		OriginModelName: "task-count-model", UsingGroup: "default",
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	info.PriceData.Quota = 80
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeBilling(c, 80, info))
	task := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	require.NoError(t, CommitTaskSubmissionWithProjection(c, info, task, 80))
	fromStatus := task.Status
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.FinishTime = 9_001
	won, err := TransitionTaskWithBilling(context.Background(), task, fromStatus, 120, "actual token usage")
	require.NoError(t, err)
	assert.True(t, won)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 120, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.Equal(t, int64(120), channel.UsedQuota)
	assert.Equal(t, int64(2), countLogs(t))
}

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	if err := model.InitLogDB(); err != nil {
		panic("failed to initialize database column names: " + err.Error())
	}
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.Midjourney{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.Channel{},
		&model.QuotaData{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.BillingRefundIntent{},
		&model.BillingAdjustmentIntent{},
		&model.BillingSettlementEvent{},
		&model.BillingTerminalRecovery{},
		&model.BillingProjectionOutbox{},
		&model.TaskSubmissionRecovery{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}
	if err := model.EnsureTaskSubmissionIdempotencyUniqueIndex(db); err != nil {
		panic("failed to migrate task submission idempotency: " + err.Error())
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_quota_data_bucket_key ON quota_data (bucket_key)").Error; err != nil {
		panic("failed to migrate quota data bucket identity: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM quota_data")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM subscription_pre_consume_records")
		model.DB.Exec("DELETE FROM subscription_plans")
		model.DB.Exec("DELETE FROM billing_refund_intents")
		model.DB.Exec("DELETE FROM billing_adjustment_intents")
		model.DB.Exec("DELETE FROM billing_settlement_events")
		model.DB.Exec("DELETE FROM billing_terminal_recoveries")
		model.DB.Exec("DELETE FROM billing_projection_outboxes")
		model.DB.Exec("DELETE FROM task_submission_recoveries")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int, usedQuota ...int) {
	t.Helper()
	used := 0
	if len(usedQuota) > 0 {
		used = usedQuota[0]
	}
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   used,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RefundTaskQuota(ctx, task, "task failed: upstream error")

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain, preConsumed)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RefundTaskQuota(ctx, task, "subscription task failed")

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	RefundTaskQuota(ctx, task, "zero quota task")

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0

	RefundTaskQuota(ctx, task, "no token task failed")

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	// No change (early return)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain, preConsumed)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling drives the production terminal CAS + durable adjustment
// entry point used by every task polling path.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	fromStatus := task.Status
	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		actualQuota = 0
	default:
		task.Progress = "50%"
		_, _ = task.UpdateWithStatus(fromStatus)
		return
	}
	_, _ = TransitionTaskWithBilling(ctx, task, fromStatus, actualQuota, task.FailReason)
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(context.Context, string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no recalculation by tokens
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestTransitionTaskWithBillingCrashGapReconcilesOnce(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 80, 80, 80
	const preConsumed = 3000
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-pending-token", 5000, preConsumed)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	fromStatus := task.Status
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = "upstream failed"

	const callbackName = "test:fail_task_adjustment_token_apply"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Token" {
			tx.AddError(errors.New("injected token apply failure"))
		}
	}))
	won, err := TransitionTaskWithBilling(ctx, task, fromStatus, 0, task.FailReason)
	assert.True(t, won)
	require.ErrorContains(t, err, "injected token apply failure")
	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), persisted.Status)
	assert.Zero(t, persisted.Quota)
	assert.Equal(t, 10000, getUserQuota(t, userID), "failed apply must not partially refund wallet")
	assert.Equal(t, 5000, getTokenRemainQuota(t, tokenID))
	var intent model.BillingAdjustmentIntent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", fmt.Sprintf("task:%d", task.ID), "task_final_adjustment").First(&intent).Error)
	assert.Equal(t, model.BillingAdjustmentStatusPending, intent.Status)

	applied, reconcileErr := model.ReconcilePendingBillingSettlements(10)
	require.NoError(t, reconcileErr)
	assert.Equal(t, 1, applied)
	assert.Equal(t, 13000, getUserQuota(t, userID))
	assert.Equal(t, 8000, getTokenRemainQuota(t, tokenID))
	applied, reconcileErr = model.ReconcilePendingBillingSettlements(10)
	require.NoError(t, reconcileErr)
	assert.Zero(t, applied)
	assert.Equal(t, 13000, getUserQuota(t, userID))
	var outbox model.BillingProjectionOutbox
	projectionKey := model.BillingProjectionKey(fmt.Sprintf("task:%d", task.ID), billingSettlementOperation, "task_terminal_adjustment")
	require.NoError(t, model.DB.Where("projection_key = ?", projectionKey).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusPending, outbox.Status)
	projected, projectionErr := model.ReconcilePendingBillingProjections(10)
	require.NoError(t, projectionErr)
	assert.Equal(t, 1, projected)
	projected, projectionErr = model.ReconcilePendingBillingProjections(10)
	require.NoError(t, projectionErr)
	assert.Zero(t, projected)
	assert.Equal(t, int64(1), countLogs(t))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
}

func TestTransitionTaskWithBillingConcurrentTerminalWinner(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 81, 81, 81
	const preConsumed = 2000
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-concurrent-token", 5000, preConsumed)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	const workers = 6
	start := make(chan struct{})
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var candidate model.Task
			if err := model.DB.First(&candidate, task.ID).Error; err != nil {
				errs <- err
				return
			}
			candidate.Status = model.TaskStatusFailure
			candidate.Progress = "100%"
			candidate.FailReason = "concurrent failure"
			<-start
			won, err := TransitionTaskWithBilling(ctx, &candidate, model.TaskStatusInProgress, 0, candidate.FailReason)
			results <- won
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	for won := range results {
		if won {
			winners++
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, winners)
	assert.Equal(t, 12000, getUserQuota(t, userID))
	assert.Equal(t, 7000, getTokenRemainQuota(t, tokenID))
	var intents int64
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentIntent{}).
		Where("request_id = ? AND operation = ?", fmt.Sprintf("task:%d", task.ID), "task_final_adjustment").Count(&intents).Error)
	assert.Equal(t, int64(1), intents)
}
