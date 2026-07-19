package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type midjourneyRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip midjourneyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTrip(req)
}

type midjourneyCloseTrackingBody struct {
	io.Reader
	closed bool
}

func (body *midjourneyCloseTrackingBody) Close() error {
	body.closed = true
	return nil
}

func TestCoverPlusActionToNormalActionRejectsMalformedCustomIDs(t *testing.T) {
	tests := []struct {
		name        string
		request     *dto.MidjourneyRequest
		description string
	}{
		{name: "nil request", description: "invalid_request"},
		{name: "no delimiter", request: &dto.MidjourneyRequest{CustomId: "MJ"}, description: "unknown_action"},
		{name: "job without action", request: &dto.MidjourneyRequest{CustomId: "MJ::JOB"}, description: "unknown_action"},
		{name: "upsample without index", request: &dto.MidjourneyRequest{CustomId: "MJ::JOB::upsample"}, description: "index_parse_failed"},
		{name: "variation without index", request: &dto.MidjourneyRequest{CustomId: "MJ::JOB::variation"}, description: "index_parse_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := CoverPlusActionToNormalAction(test.request)
			require.NotNil(t, response)
			assert.Equal(t, test.description, response.Description)
		})
	}
}

func TestConvertSimpleChangeParamsRejectsShortActions(t *testing.T) {
	for _, input := range []string{"", "task", "task ", " u1", "task u", "task v"} {
		t.Run(input, func(t *testing.T) {
			assert.Nil(t, ConvertSimpleChangeParams(input))
		})
	}

	upscale := ConvertSimpleChangeParams("task u1")
	require.NotNil(t, upscale)
	assert.Equal(t, "UPSCALE", upscale.Action)
	assert.Equal(t, 1, upscale.Index)
	reroll := ConvertSimpleChangeParams("task r")
	require.NotNil(t, reroll)
	assert.Equal(t, "REROLL", reroll.Action)
}

func TestFetchMidjourneyTasksClosesNonOKBodyAndAddsFiniteDeadline(t *testing.T) {
	originalClient := httpClient
	body := &midjourneyCloseTrackingBody{Reader: strings.NewReader("provider unavailable")}
	httpClient = &http.Client{Transport: midjourneyRoundTripper(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		assert.True(t, ok, "poll requests must always have a finite deadline")
		assert.LessOrEqual(t, time.Until(deadline), midjourneyTaskPollingTimeout)
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body, Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { httpClient = originalClient })

	_, err := FetchMidjourneyTasks(context.Background(), "https://midjourney.invalid", "secret", []string{"task"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 502")
	assert.NotContains(t, err.Error(), "provider unavailable")
	assert.True(t, body.closed, "non-200 poll response body must be closed")
}

func TestFetchMidjourneyTasksFailsSafelyWithoutHTTPClient(t *testing.T) {
	originalClient := httpClient
	httpClient = nil
	t.Cleanup(func() { httpClient = originalClient })

	_, err := FetchMidjourneyTasks(context.Background(), "https://midjourney.invalid", "secret", []string{"task"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestDoMidjourneyHttpRequestUsesCallerCancellation(t *testing.T) {
	originalClient := httpClient
	requestStarted := make(chan struct{})
	httpClient = &http.Client{Transport: midjourneyRoundTripper(func(req *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	t.Cleanup(func() { httpClient = originalClient })

	ctx, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", strings.NewReader(`{"prompt":"test"}`)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	result := make(chan error, 1)
	go func() {
		_, _, err := DoMidjourneyHttpRequest(c, time.Minute, "https://midjourney.invalid/mj/submit")
		result <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Midjourney request did not start")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "unexpected cancellation error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Midjourney request ignored caller cancellation")
	}
}
