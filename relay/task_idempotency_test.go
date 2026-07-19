package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskIdempotencyRelayDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TaskSubmissionRecovery{}))
	require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN tenant_id BIGINT NULL").Error)
	require.NoError(t, model.EnsureTaskSubmissionIdempotencyUniqueIndex(db))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})
}

func taskIdempotencyContext(t *testing.T, requestId string, key string, body string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "https://tenant.example.com/mj/submit/imagine?mode=fast", strings.NewReader(body))
	c.Request.Host = "Tenant.Example.com:443"
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Accept", "application/json")
	c.Request.Header.Set("Idempotency-Key", key)
	info := &relaycommon.RelayInfo{
		RequestId: requestId, UserId: 7301, TokenId: 0, OriginModelName: "midjourney",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 44},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_investigation_public", Action: "IMAGINE"},
	}
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder, info
}

func TestTaskSubmissionConcurrentClaimAllowsOneProviderCall(t *testing.T) {
	setupTaskIdempotencyRelayDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 7301, Username: "claim-user"}).Error)
	const workers = 10
	start := make(chan struct{})
	var providerCalls atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			c, _, info := taskIdempotencyContext(t, fmt.Sprintf("relay-claim-%d", index), "relay-concurrent-key", `{"prompt":"same"}`)
			spec, err := buildTaskSubmissionClaimSpec(c, info, model.TaskSubmissionKindMidjourney, info.PublicTaskID)
			if err != nil {
				errorsSeen <- err
				return
			}
			<-start
			state, err := claimTaskSubmission(c, info, spec)
			if err == nil && state.Recovery != nil && info.TaskSubmissionClaimOwned {
				providerCalls.Add(1)
			}
			if err != nil && !errorsIsIdempotencyInProgress(err) {
				errorsSeen <- err
				return
			}
			errorsSeen <- nil
		}(index)
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), providerCalls.Load())
}

func errorsIsIdempotencyInProgress(err error) bool {
	return err == model.ErrTaskSubmissionIdempotencyInProgress
}

func TestCommittedReplayBypassesSaturatedResponseCapacity(t *testing.T) {
	setupTaskIdempotencyRelayDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 7301, Username: "replay-user"}).Error)
	firstContext, _, firstInfo := taskIdempotencyContext(t, "relay-replay-owner", "relay-replay-key", `{"prompt":"same"}`)
	spec, err := buildTaskSubmissionClaimSpec(firstContext, firstInfo, model.TaskSubmissionKindMidjourney, firstInfo.PublicTaskID)
	require.NoError(t, err)
	claim, err := model.ClaimTaskSubmission(spec)
	require.NoError(t, err)
	require.True(t, claim.Owned)
	require.NoError(t, model.MarkTaskSubmissionUncertainWithMetadata(claim.Recovery.RequestId, model.TaskSubmissionKindMidjourney,
		model.TaskSubmissionAttemptMetadata{
			UserId: 7301, TokenId: 0, ChannelId: 44, Provider: "midjourney", Model: "midjourney",
			PublicTaskId: "task_investigation_public", Action: "IMAGINE", UsingGroup: "default", InitialQuota: 0,
		}))
	require.NoError(t, model.FreezeAcceptedTaskSubmission(claim.Recovery.RequestId, model.TaskSubmissionKindMidjourney,
		model.TaskSubmissionCommitPayload{
			Midjourney:        &model.Midjourney{UserId: 7301, MjId: "mj-replay-provider", Action: "IMAGINE"},
			MidjourneyBilling: &model.MidjourneySubmissionBilling{BillingSource: "free", BillingRequestId: claim.Recovery.RequestId},
			PublicResponse: &model.TaskSubmissionPublicResponse{
				Status: 201, Headers: map[string][]string{"Content-Type": {"application/json"}, "X-Oneapi-Request-Id": {"safe-request-id"}},
				Body: `{"code":1,"result":"mj-replay-provider"}`,
			},
		}))

	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "1024")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "1024")
	saturation, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)
	defer saturation.Release()

	replayContext, recorder, replayInfo := taskIdempotencyContext(t, "relay-replay-retry", "relay-replay-key", `{"prompt":"same"}`)
	reservation, replayed, recoveryErr := prepareMidjourneySubmissionRecovery(replayContext, replayInfo)
	require.Nil(t, recoveryErr)
	assert.Nil(t, reservation)
	assert.True(t, replayed)
	assert.Equal(t, 201, recorder.Code)
	assert.Equal(t, "safe-request-id", recorder.Header().Get("X-Oneapi-Request-Id"))
	assert.Equal(t, `{"code":1,"result":"mj-replay-provider"}`, recorder.Body.String())
}

func TestCapacityFailureDoesNotBurnNewIdempotencyKey(t *testing.T) {
	setupTaskIdempotencyRelayDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 7301, Username: "capacity-user"}).Error)
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "1024")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "1024")
	saturation, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)

	blockedContext, _, blockedInfo := taskIdempotencyContext(t, "capacity-blocked", "capacity-new-key", `{"prompt":"same"}`)
	reservation, replayed, recoveryErr := prepareMidjourneySubmissionRecovery(blockedContext, blockedInfo)
	require.NotNil(t, recoveryErr)
	assert.Nil(t, reservation)
	assert.False(t, replayed)
	var rows int64
	require.NoError(t, model.DB.Model(&model.TaskSubmissionRecovery{}).Count(&rows).Error)
	assert.Zero(t, rows)
	saturation.Release()

	retryContext, _, retryInfo := taskIdempotencyContext(t, "capacity-retry", "capacity-new-key", `{"prompt":"same"}`)
	reservation, replayed, recoveryErr = prepareMidjourneySubmissionRecovery(retryContext, retryInfo)
	require.Nil(t, recoveryErr)
	require.NotNil(t, reservation)
	defer reservation.Release()
	assert.False(t, replayed)
	require.NoError(t, model.DB.Model(&model.TaskSubmissionRecovery{}).Count(&rows).Error)
	assert.Equal(t, int64(1), rows)
}
