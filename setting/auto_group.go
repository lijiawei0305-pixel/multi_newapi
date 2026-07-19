package setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var autoGroups = []string{
	"default",
}
var autoGroupsMutex sync.RWMutex

var DefaultUseAutoGroup = false

func ContainsAutoGroup(group string) bool {
	autoGroupsMutex.RLock()
	defer autoGroupsMutex.RUnlock()
	for _, autoGroup := range autoGroups {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	updated := make([]string, 0)
	if err := common.Unmarshal([]byte(jsonString), &updated); err != nil {
		return err
	}
	autoGroupsMutex.Lock()
	autoGroups = updated
	autoGroupsMutex.Unlock()
	return nil
}

func AutoGroups2JsonString() string {
	autoGroupsMutex.RLock()
	defer autoGroupsMutex.RUnlock()
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	autoGroupsMutex.RLock()
	defer autoGroupsMutex.RUnlock()
	return append([]string(nil), autoGroups...)
}
