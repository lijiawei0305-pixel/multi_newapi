package model

func GetModelEnableGroups(modelName string) []string {
	// 确保缓存最新
	GetPricing()

	if modelName == "" {
		return make([]string, 0)
	}

	pricingCacheLock.RLock()
	groups, ok := pricingCache.modelEnableGroups[modelName]
	groups = append([]string(nil), groups...)
	pricingCacheLock.RUnlock()
	if !ok {
		return make([]string, 0)
	}
	return groups
}

// GetModelQuotaTypes 返回指定模型的计费类型集合（来自缓存）
func GetModelQuotaTypes(modelName string) []int {
	GetPricing()

	pricingCacheLock.RLock()
	quota, ok := pricingCache.modelQuotaTypes[modelName]
	pricingCacheLock.RUnlock()
	if !ok {
		return []int{}
	}
	return []int{quota}
}
