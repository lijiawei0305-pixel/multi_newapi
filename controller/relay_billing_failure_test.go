package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type acceptedBillingFailureSession struct {
	refundCalls int
}

func (s *acceptedBillingFailureSession) Settle(int) error { return nil }

func (s *acceptedBillingFailureSession) Refund(*gin.Context) error {
	s.refundCalls++
	return nil
}

func (s *acceptedBillingFailureSession) NeedsRefund() bool { return true }

func (s *acceptedBillingFailureSession) GetPreConsumedQuota() int { return 60 }

func (s *acceptedBillingFailureSession) Reserve(int) error { return nil }

func TestAcceptedBillingFreezeFailureSkipsRefundRetryAndBufferedSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	deferred, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = deferred
	c.JSON(http.StatusOK, gin.H{"accepted": true, "secret_upstream_body": "must not leak"})

	session := &acceptedBillingFailureSession{}
	info := &relaycommon.RelayInfo{Billing: session}
	service.MarkUpstreamAccepted(c)
	service.MarkBillingTerminalAttempted(c)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("upstream accepted but billing terminal state is unknown"),
		types.ErrorCodeUpdateDataError,
		http.StatusInternalServerError,
	)

	apiErr = finalizeRelayBillingFailure(c, info, apiErr)
	assert.Zero(t, session.refundCalls)
	assert.False(t, shouldRetry(c, apiErr, 3), "accepted upstream work is never retried even if the error lacks a skip-retry option")
	finishRelayResponse(c, types.RelayFormatOpenAI, nil, "accepted-freeze-failure", deferred, apiErr)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	body := recorder.Body.String()
	assert.NotContains(t, body, "secret_upstream_body")
	assert.NotContains(t, body, `"accepted":true`)
	assert.True(t, strings.Contains(body, "billing terminal state is unknown"), body)
}

func TestStreamingAcceptedBillingFailureDoesNotAppendJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Header("Content-Type", "text/event-stream")
	_, err := c.Writer.Write([]byte("data: upstream-success\n\n"))
	require.NoError(t, err)
	service.MarkUpstreamAccepted(c)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("post-stream billing failed"), types.ErrorCodeUpdateDataError,
		http.StatusInternalServerError, types.ErrOptionWithSkipRetry(),
	)

	finishRelayResponse(c, types.RelayFormatOpenAI, nil, "stream-billing-failure", nil, apiErr)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "data: upstream-success\n\n", recorder.Body.String())
}

func TestRelayRetryDiscardsFailedAttemptResponseBeforeSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	firstAttempt, err := beginRelayAttemptResponse(c, true)
	require.NoError(t, err)
	firstAttempt.Header().Set("X-Upstream-Attempt", "first")
	firstAttempt.WriteHeader(http.StatusBadGateway)
	_, err = firstAttempt.WriteString("partial-first-attempt")
	require.NoError(t, err)
	discardRelayAttemptResponse(c, firstAttempt)
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("X-Upstream-Attempt"))

	secondAttempt, err := beginRelayAttemptResponse(c, true)
	require.NoError(t, err)
	secondAttempt.Header().Set("X-Upstream-Attempt", "second")
	secondAttempt.WriteHeader(http.StatusOK)
	_, err = secondAttempt.WriteString("complete-second-attempt")
	require.NoError(t, err)
	finishRelayResponse(c, types.RelayFormatOpenAI, nil, "retry-success", secondAttempt, nil)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "second", recorder.Header().Get("X-Upstream-Attempt"))
	assert.Equal(t, "complete-second-attempt", recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "partial-first-attempt")
}

func TestRelayResponseAdmissionFailureNeverInvokesProvider(t *testing.T) {
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")

	held, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)
	t.Cleanup(held.Release)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var admitted *common.BufferedResponseWriter
	providerCalls := 0
	apiErr, err := invokeRelayAttemptAfterAdmission(c, true, &admitted, func() *types.NewAPIError {
		providerCalls++
		return nil
	})

	require.ErrorIs(t, err, common.ErrBufferedResponseCapacity)
	assert.Nil(t, apiErr)
	assert.Nil(t, admitted)
	assert.Zero(t, providerCalls)
}

func TestRelayResponseAdmissionErrorDoesNotExposeCapacityOrTempPath(t *testing.T) {
	privateCause := errors.New("spool /private/tmp/new-api-response: reserved=268435456 available=0")

	apiErr := relayResponseAdmissionError(privateCause)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.NotContains(t, apiErr.Error(), "/private/tmp")
	assert.NotContains(t, apiErr.Error(), "268435456")
	assert.NotContains(t, apiErr.Error(), "available=0")
}

func TestRelayPanicBeforeUpstreamAcceptanceRefundsReservation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &acceptedBillingFailureSession{}
	compensateRelayPanic(c, &relaycommon.RelayInfo{Billing: session})
	assert.Equal(t, 1, session.refundCalls)
}

func TestRelayPanicAfterUpstreamAcceptanceNeverRefunds(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &acceptedBillingFailureSession{}
	service.MarkUpstreamAccepted(c)
	compensateRelayPanic(c, &relaycommon.RelayInfo{Billing: session})
	assert.Zero(t, session.refundCalls)
}

func TestAcceptedRelayPanicFinalizesBillingAndAlwaysReleasesResponseAdmission(t *testing.T) {
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	buffered, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = buffered
	session := &acceptedBillingFailureSession{}
	info := &relaycommon.RelayInfo{Billing: session}
	service.MarkUpstreamAccepted(c)

	originalFinalizer := finalizeAcceptedBillingFailure
	finalizeCalls := 0
	finalizeAcceptedBillingFailure = func(ctx *gin.Context, gotInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
		finalizeCalls++
		assert.Same(t, info, gotInfo)
		assert.True(t, types.IsSkipRetryError(apiErr))
		service.MarkBillingTerminalAttempted(ctx)
		return apiErr
	}
	t.Cleanup(func() { finalizeAcceptedBillingFailure = originalFinalizer })

	cleanupRelayPanic(c, info, buffered)

	assert.Equal(t, 1, finalizeCalls)
	assert.True(t, service.IsBillingTerminalAttempted(c))
	assert.Zero(t, session.refundCalls)
	assert.Same(t, buffered.Underlying(), c.Writer)
	replacement, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)
	replacement.Release()
}

func TestAcceptedStreamingPanicDoesNotAppendErrorPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	_, err := c.Writer.Write([]byte("data: accepted-audio\n\n"))
	require.NoError(t, err)
	service.MarkUpstreamAccepted(c)
	service.MarkBillingTerminalAttempted(c)
	bodyBeforePanic := recorder.Body.String()

	suppressed := handleRelayPanic(c, &relaycommon.RelayInfo{}, nil, nil)

	assert.True(t, suppressed)
	assert.True(t, c.IsAborted())
	assert.Equal(t, bodyBeforePanic, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), `"error"`)
}

func TestTaskSubmissionDurableACKCommitsBufferedSuccessDespiteApplyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	buffer, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	buffer.Header().Set("Content-Type", "application/json")
	_, err = buffer.WriteString(`{"id":"task_public_safe"}`)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{RequestId: "task-durable-request"}
	result := &relay.TaskSubmitResult{Quota: 10, ClientResponse: buffer}
	privateSecret := "database-password-must-not-leak"
	taskErr := finalizeTaskSubmissionResponse(c, info, result, &model.Task{}, func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *model.Task, _ int, response *model.TaskSubmissionPublicResponse) (service.TaskSubmissionCommitOutcome, error) {
		require.NotNil(t, response)
		return service.TaskSubmissionCommitOutcome{Durable: true}, errors.New(privateSecret)
	})
	require.Nil(t, taskErr)
	assert.True(t, info.TaskSubmissionRecoveryProtected)
	assert.False(t, taskSubmissionRetryAllowed(info))
	assert.Equal(t, `{"id":"task_public_safe"}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), privateSecret)
}

func TestTaskSubmissionFreezeFailureDiscardsSuccessAndReturnsSafeUnknownState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	buffer, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	_, err = buffer.WriteString(`{"id":"must_be_discarded"}`)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{RequestId: "task-freeze-request", Billing: &acceptedBillingFailureSession{}}
	result := &relay.TaskSubmitResult{Quota: 10, ClientResponse: buffer}
	privateSecret := "postgres://admin:secret@db.internal"
	taskErr := finalizeTaskSubmissionResponse(c, info, result, &model.Task{}, func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *model.Task, _ int, response *model.TaskSubmissionPublicResponse) (service.TaskSubmissionCommitOutcome, error) {
		require.NotNil(t, response)
		return service.TaskSubmissionCommitOutcome{}, errors.New(privateSecret)
	})
	require.NotNil(t, taskErr)
	assert.True(t, info.TaskSubmissionRecoveryProtected)
	assert.False(t, taskSubmissionRetryAllowed(info))
	assert.NotContains(t, taskErr.Message, privateSecret)
	assert.NotContains(t, taskErr.Message, "query")
	assert.Contains(t, taskErr.Message, "do not retry")
	assert.Empty(t, recorder.Body.String())

	respondTaskError(c, taskErr)
	assert.NotContains(t, recorder.Body.String(), privateSecret)
	assert.NotContains(t, recorder.Body.String(), "must_be_discarded")
	cleanupFailedTaskSubmission(c, info)
	assert.Zero(t, info.Billing.(*acceptedBillingFailureSession).refundCalls, "protected accepted state cannot refund")
}

func TestTaskSubmissionPanicCleanupRespectsRecoveryBoundary(t *testing.T) {
	t.Run("before send atomically aborts", func(t *testing.T) {
		info := &relaycommon.RelayInfo{
			RequestId: "panic-before-send", TaskSubmissionRecoveryKind: model.TaskSubmissionKindTask,
			TaskSubmissionRecoveryPrepared: true, Billing: &acceptedBillingFailureSession{},
		}
		abortCalls := 0
		cleanupFailedTaskSubmissionWithAbort(nil, info, func(requestId string, kind string) error {
			abortCalls++
			assert.Equal(t, info.RequestId, requestId)
			assert.Equal(t, model.TaskSubmissionKindTask, kind)
			return nil
		})
		assert.Equal(t, 1, abortCalls)
		assert.Zero(t, info.Billing.(*acceptedBillingFailureSession).refundCalls, "atomic abort owns the durable refund")
	})

	t.Run("after send never aborts or refunds", func(t *testing.T) {
		info := &relaycommon.RelayInfo{
			RequestId: "panic-after-send", TaskSubmissionRecoveryKind: model.TaskSubmissionKindTask,
			TaskSubmissionRecoveryPrepared: true, TaskSubmissionRecoveryProtected: true,
			Billing: &acceptedBillingFailureSession{},
		}
		abortCalls := 0
		cleanupFailedTaskSubmissionWithAbort(nil, info, func(string, string) error {
			abortCalls++
			return nil
		})
		assert.Zero(t, abortCalls)
		assert.Zero(t, info.Billing.(*acceptedBillingFailureSession).refundCalls)
	})
}
