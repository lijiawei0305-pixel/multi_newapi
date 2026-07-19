package setting

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateModelRequestRateLimitGroupByJSONStringPreservesValueOnInvalidJSON(t *testing.T) {
	restoreModelRequestRateLimitConfig(t)

	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"existing":[100,90]}`))
	err := UpdateModelRequestRateLimitGroupByJSONString(`{"replacement":[200,180]`)
	require.Error(t, err)

	total, success, found := GetGroupRateLimit("existing")
	assert.True(t, found)
	assert.Equal(t, 100, total)
	assert.Equal(t, 90, success)
	_, _, found = GetGroupRateLimit("replacement")
	assert.False(t, found)
}

func TestModelRequestRateLimitConfigConcurrentAccessNeverMixesVersions(t *testing.T) {
	restoreModelRequestRateLimitConfig(t)

	const (
		groupAJSON = `{"shared":[100,90],"group-a":[40,30]}`
		groupBJSON = `{"shared":[200,180],"group-b":[60,50]}`
	)
	groupA := map[string][2]int{
		"shared":  {100, 90},
		"group-a": {40, 30},
	}
	groupB := map[string][2]int{
		"shared":  {200, 180},
		"group-b": {60, 50},
	}
	optionsA := map[string]string{
		"ModelRequestRateLimitEnabled":         "true",
		"ModelRequestRateLimitDurationMinutes": "10",
		"ModelRequestRateLimitCount":           "100",
		"ModelRequestRateLimitSuccessCount":    "90",
		"ModelRequestRateLimitGroup":           groupAJSON,
	}
	optionsB := map[string]string{
		"ModelRequestRateLimitEnabled":         "false",
		"ModelRequestRateLimitDurationMinutes": "20",
		"ModelRequestRateLimitCount":           "200",
		"ModelRequestRateLimitSuccessCount":    "180",
		"ModelRequestRateLimitGroup":           groupBJSON,
	}
	_, err := ApplyModelRequestRateLimitOptions(optionsA)
	require.NoError(t, err)

	start := make(chan struct{})
	errCh := make(chan error, 16)
	var workers sync.WaitGroup

	for writer := 0; writer < 4; writer++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 500; iteration++ {
				options := optionsA
				if iteration%2 == 1 {
					options = optionsB
				}
				if _, err := ApplyModelRequestRateLimitOptions(options); err != nil {
					errCh <- fmt.Errorf("update rate-limit group: %w", err)
					return
				}
			}
		}()
	}

	for reader := 0; reader < 8; reader++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 500; iteration++ {
				config := GetModelRequestRateLimitConfig()
				var snapshot map[string][2]int
				if err := common.Unmarshal([]byte(config.GroupRateLimitsJSONString()), &snapshot); err != nil {
					errCh <- fmt.Errorf("decode rate-limit snapshot: %w", err)
					return
				}
				isA := config.Enabled && config.DurationMinutes == 10 && config.Count == 100 &&
					config.SuccessCount == 90 && reflect.DeepEqual(snapshot, groupA)
				isB := !config.Enabled && config.DurationMinutes == 20 && config.Count == 200 &&
					config.SuccessCount == 180 && reflect.DeepEqual(snapshot, groupB)
				if !isA && !isB {
					errCh <- fmt.Errorf("observed partial rate-limit snapshot: %#v", snapshot)
					return
				}

				total, success, found := config.GroupLimit("shared")
				if !found || isA && (total != 100 || success != 90) || isB && (total != 200 || success != 180) {
					errCh <- fmt.Errorf("observed invalid shared limit: total=%d success=%d found=%t", total, success, found)
					return
				}
			}
		}()
	}

	close(start)
	workers.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

func restoreModelRequestRateLimitConfig(t *testing.T) {
	t.Helper()
	original := GetModelRequestRateLimitConfig()

	t.Cleanup(func() {
		_, err := ApplyModelRequestRateLimitOptions(map[string]string{
			"ModelRequestRateLimitEnabled":         fmt.Sprintf("%t", original.Enabled),
			"ModelRequestRateLimitDurationMinutes": fmt.Sprintf("%d", original.DurationMinutes),
			"ModelRequestRateLimitCount":           fmt.Sprintf("%d", original.Count),
			"ModelRequestRateLimitSuccessCount":    fmt.Sprintf("%d", original.SuccessCount),
			"ModelRequestRateLimitGroup":           original.GroupRateLimitsJSONString(),
		})
		require.NoError(t, err)
	})
}
