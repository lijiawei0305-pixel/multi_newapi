package vertex

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func GetModelRegion(other string, localModelName string) string {
	if !common.IsJsonObject(other) {
		return other
	}

	regions, err := common.StrToMap(other)
	if err != nil {
		return "global"
	}
	if region, ok := regions[localModelName].(string); ok && strings.TrimSpace(region) != "" {
		return region
	}
	if region, ok := regions["default"].(string); ok && strings.TrimSpace(region) != "" {
		return region
	}
	return "global"
}
