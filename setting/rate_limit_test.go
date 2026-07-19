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
	restoreModelRequestRateLimitGroup(t)

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

func TestUpdateModelRequestRateLimitGroupByJSONStringConcurrentAccess(t *testing.T) {
	restoreModelRequestRateLimitGroup(t)

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
	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(groupAJSON))

	start := make(chan struct{})
	errCh := make(chan error, 16)
	var workers sync.WaitGroup

	for writer := 0; writer < 4; writer++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 500; iteration++ {
				jsonValue := groupAJSON
				if iteration%2 == 1 {
					jsonValue = groupBJSON
				}
				if err := UpdateModelRequestRateLimitGroupByJSONString(jsonValue); err != nil {
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
				var snapshot map[string][2]int
				if err := common.Unmarshal([]byte(ModelRequestRateLimitGroup2JSONString()), &snapshot); err != nil {
					errCh <- fmt.Errorf("decode rate-limit snapshot: %w", err)
					return
				}
				if !reflect.DeepEqual(snapshot, groupA) && !reflect.DeepEqual(snapshot, groupB) {
					errCh <- fmt.Errorf("observed partial rate-limit snapshot: %#v", snapshot)
					return
				}

				total, success, found := GetGroupRateLimit("shared")
				if !found || (total != 100 || success != 90) && (total != 200 || success != 180) {
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

func restoreModelRequestRateLimitGroup(t *testing.T) {
	t.Helper()

	ModelRequestRateLimitMutex.RLock()
	original := make(map[string][2]int, len(ModelRequestRateLimitGroup))
	for group, limits := range ModelRequestRateLimitGroup {
		original[group] = limits
	}
	ModelRequestRateLimitMutex.RUnlock()

	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = original
		ModelRequestRateLimitMutex.Unlock()
	})
}
