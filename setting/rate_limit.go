package setting

import (
	"fmt"
	"math"
	"strconv"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

// ModelRequestRateLimitConfig is an immutable runtime snapshot. The group map
// is deliberately private and is never mutated after the snapshot is
// published, allowing one request to use a consistent scalar-and-group view.
type ModelRequestRateLimitConfig struct {
	Enabled         bool
	DurationMinutes int
	Count           int
	SuccessCount    int
	groups          map[string][2]int
}

var modelRequestRateLimitConfig atomic.Pointer[ModelRequestRateLimitConfig]

func defaultModelRequestRateLimitConfig() ModelRequestRateLimitConfig {
	return ModelRequestRateLimitConfig{
		DurationMinutes: 1,
		SuccessCount:    1000,
		groups:          map[string][2]int{},
	}
}

// GetModelRequestRateLimitConfig returns one immutable value snapshot.
func GetModelRequestRateLimitConfig() ModelRequestRateLimitConfig {
	config := modelRequestRateLimitConfig.Load()
	if config == nil {
		return defaultModelRequestRateLimitConfig()
	}
	return *config
}

// GroupLimit looks up a group override from this exact snapshot version.
func (config ModelRequestRateLimitConfig) GroupLimit(group string) (totalCount, successCount int, found bool) {
	limits, found := config.groups[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

// GroupRateLimitsJSONString serializes the group overrides from this exact
// snapshot version.
func (config ModelRequestRateLimitConfig) GroupRateLimitsJSONString() string {
	jsonBytes, err := common.Marshal(config.groups)
	if err != nil {
		common.SysLog("error marshalling model request rate limits: " + err.Error())
	}
	return string(jsonBytes)
}

// IsModelRequestRateLimitOption reports whether key belongs to the coupled
// model-request rate-limit snapshot.
func IsModelRequestRateLimitOption(key string) bool {
	switch key {
	case "ModelRequestRateLimitEnabled", "ModelRequestRateLimitDurationMinutes",
		"ModelRequestRateLimitCount", "ModelRequestRateLimitSuccessCount", "ModelRequestRateLimitGroup":
		return true
	default:
		return false
	}
}

// ApplyModelRequestRateLimitOptions validates all recognized values, applies
// them to one copy, and atomically publishes the complete snapshot.
func ApplyModelRequestRateLimitOptions(values map[string]string) (bool, error) {
	var enabled *bool
	var durationMinutes *int
	var count *int
	var successCount *int
	var groups map[string][2]int
	groupsSet := false
	handled := false

	for key, value := range values {
		switch key {
		case "ModelRequestRateLimitEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, fmt.Errorf("invalid ModelRequestRateLimitEnabled: %w", err)
			}
			enabled = &parsed
			handled = true
		case "ModelRequestRateLimitDurationMinutes":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("invalid ModelRequestRateLimitDurationMinutes: %w", err)
			}
			durationMinutes = &parsed
			handled = true
		case "ModelRequestRateLimitCount":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("invalid ModelRequestRateLimitCount: %w", err)
			}
			count = &parsed
			handled = true
		case "ModelRequestRateLimitSuccessCount":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("invalid ModelRequestRateLimitSuccessCount: %w", err)
			}
			successCount = &parsed
			handled = true
		case "ModelRequestRateLimitGroup":
			parsed := make(map[string][2]int)
			if err := common.Unmarshal([]byte(value), &parsed); err != nil {
				return false, fmt.Errorf("invalid ModelRequestRateLimitGroup: %w", err)
			}
			if err := validateModelRequestRateLimitGroup(parsed); err != nil {
				return false, err
			}
			groups = parsed
			groupsSet = true
			handled = true
		}
	}
	if !handled {
		return false, nil
	}

	for {
		current := modelRequestRateLimitConfig.Load()
		next := defaultModelRequestRateLimitConfig()
		if current != nil {
			next = *current
		}
		if enabled != nil {
			next.Enabled = *enabled
		}
		if durationMinutes != nil {
			next.DurationMinutes = *durationMinutes
		}
		if count != nil {
			next.Count = *count
		}
		if successCount != nil {
			next.SuccessCount = *successCount
		}
		if groupsSet {
			next.groups = groups
		}

		snapshot := next
		if modelRequestRateLimitConfig.CompareAndSwap(current, &snapshot) {
			return true, nil
		}
	}
}

func ModelRequestRateLimitGroup2JSONString() string {
	return GetModelRequestRateLimitConfig().GroupRateLimitsJSONString()
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	_, err := ApplyModelRequestRateLimitOptions(map[string]string{"ModelRequestRateLimitGroup": jsonStr})
	return err
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	return GetModelRequestRateLimitConfig().GroupLimit(group)
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	groups := make(map[string][2]int)
	if err := common.Unmarshal([]byte(jsonStr), &groups); err != nil {
		return err
	}
	return validateModelRequestRateLimitGroup(groups)
}

func validateModelRequestRateLimitGroup(groups map[string][2]int) error {
	for group, limits := range groups {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}
	return nil
}
