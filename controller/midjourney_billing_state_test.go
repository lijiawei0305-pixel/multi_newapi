package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeMidjourneyTerminalStateMakesEarlyFailReasonTerminal(t *testing.T) {
	task := &model.Midjourney{
		Status:     "IN_PROGRESS",
		Progress:   "42%",
		FailReason: "provider rejected the prompt",
		Quota:      100,
	}

	assert.True(t, normalizeMidjourneyTerminalState(task))
	assert.Equal(t, "FAILURE", task.Status)
	assert.Equal(t, "100%", task.Progress)
}

func TestNormalizeMidjourneyTerminalStatePreservesCompletedSuccess(t *testing.T) {
	task := &model.Midjourney{
		Status:     "SUCCESS",
		Progress:   "100%",
		FailReason: "stale provider warning",
	}

	assert.False(t, normalizeMidjourneyTerminalState(task))
	assert.Equal(t, "SUCCESS", task.Status)
}

func TestNormalizeMidjourneyTerminalStateHandlesCompleteProgressWithoutStatus(t *testing.T) {
	task := &model.Midjourney{Progress: "100%", FailReason: "provider rejected the prompt"}

	assert.True(t, normalizeMidjourneyTerminalState(task))
	assert.Equal(t, "FAILURE", task.Status)
	assert.Equal(t, "100%", task.Progress)
}

func TestNormalizeMidjourneyTerminalStateHandlesFailureStatusWithoutReason(t *testing.T) {
	task := &model.Midjourney{Status: "FAILURE", Progress: "42%"}

	assert.True(t, normalizeMidjourneyTerminalState(task))
	assert.Equal(t, "FAILURE", task.Status)
	assert.Equal(t, "100%", task.Progress)
}

func TestNormalizeMidjourneyTerminalStateCompletesSuccessfulStatus(t *testing.T) {
	task := &model.Midjourney{Status: "SUCCESS", Progress: "90%"}

	assert.False(t, normalizeMidjourneyTerminalState(task))
	assert.Equal(t, "SUCCESS", task.Status)
	assert.Equal(t, "100%", task.Progress)
}
