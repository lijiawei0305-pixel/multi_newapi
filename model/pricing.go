package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

type Pricing struct {
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
	QuotaType              int                     `json:"quota_type"`
	ModelRatio             float64                 `json:"model_ratio"`
	ModelPrice             float64                 `json:"model_price"`
	OwnerBy                string                  `json:"owner_by"`
	CompletionRatio        float64                 `json:"completion_ratio"`
	CacheRatio             *float64                `json:"cache_ratio,omitempty"`
	CreateCacheRatio       *float64                `json:"create_cache_ratio,omitempty"`
	ImageRatio             *float64                `json:"image_ratio,omitempty"`
	AudioRatio             *float64                `json:"audio_ratio,omitempty"`
	AudioCompletionRatio   *float64                `json:"audio_completion_ratio,omitempty"`
	EnableGroup            []string                `json:"enable_groups"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	BillingMode            string                  `json:"billing_mode,omitempty"`
	BillingExpr            string                  `json:"billing_expr,omitempty"`
	PricingVersion         string                  `json:"pricing_version,omitempty"`
}

type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

type pricingCacheSnapshot struct {
	pricing                []Pricing
	vendors                []PricingVendor
	supportedEndpointTypes map[string][]constant.EndpointType
	supportedEndpoints     map[string]common.EndpointInfo
	modelEnableGroups      map[string][]string
	modelQuotaTypes        map[string]int
	lastSuccessfulRefresh  time.Time
}

var (
	pricingCache      pricingCacheSnapshot
	pricingCacheLock  sync.RWMutex
	updatePricingLock sync.Mutex
)

func GetPricing() []Pricing {
	ensurePricingCache()

	pricingCacheLock.RLock()
	defer pricingCacheLock.RUnlock()
	return clonePricingList(pricingCache.pricing)
}

func InvalidatePricingCache() {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	pricingCacheLock.Lock()
	invalidated := pricingCache
	invalidated.lastSuccessfulRefresh = time.Time{}
	pricingCache = invalidated
	pricingCacheLock.Unlock()
}

// GetVendors 返回当前定价接口使用到的供应商信息
func GetVendors() []PricingVendor {
	ensurePricingCache()

	pricingCacheLock.RLock()
	defer pricingCacheLock.RUnlock()
	return append([]PricingVendor(nil), pricingCache.vendors...)
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	pricingCacheLock.RLock()
	defer pricingCacheLock.RUnlock()
	if endpoints, ok := pricingCache.supportedEndpointTypes[model]; ok {
		return append([]constant.EndpointType(nil), endpoints...)
	}
	return make([]constant.EndpointType, 0)
}

func ensurePricingCache() {
	if !pricingCacheNeedsRefresh(time.Now()) {
		return
	}

	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	if pricingCacheNeedsRefresh(time.Now()) {
		updatePricing()
	}
}

func pricingCacheNeedsRefresh(now time.Time) bool {
	pricingCacheLock.RLock()
	defer pricingCacheLock.RUnlock()
	return len(pricingCache.pricing) == 0 || now.Sub(pricingCache.lastSuccessfulRefresh) > time.Minute
}

func clonePricingList(source []Pricing) []Pricing {
	if source == nil {
		return nil
	}
	result := make([]Pricing, len(source))
	for i := range source {
		result[i] = source[i]
		result[i].EnableGroup = append([]string(nil), source[i].EnableGroup...)
		result[i].SupportedEndpointTypes = append([]constant.EndpointType(nil), source[i].SupportedEndpointTypes...)
		result[i].CacheRatio = cloneFloat64(source[i].CacheRatio)
		result[i].CreateCacheRatio = cloneFloat64(source[i].CreateCacheRatio)
		result[i].ImageRatio = cloneFloat64(source[i].ImageRatio)
		result[i].AudioRatio = cloneFloat64(source[i].AudioRatio)
		result[i].AudioCompletionRatio = cloneFloat64(source[i].AudioCompletionRatio)
	}
	return result
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func updatePricing() {
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	_ = DB.Find(&allMeta).Error
	metaMap := make(map[string]*Model)
	prefixList := make([]*Model, 0)
	suffixList := make([]*Model, 0)
	containsList := make([]*Model, 0)
	for i := range allMeta {
		m := &allMeta[i]
		if m.NameRule == NameRuleExact {
			metaMap[m.ModelName] = m
		} else {
			switch m.NameRule {
			case NameRulePrefix:
				prefixList = append(prefixList, m)
			case NameRuleSuffix:
				suffixList = append(suffixList, m)
			case NameRuleContains:
				containsList = append(containsList, m)
			}
		}
	}

	// 将非精确规则模型匹配到 metaMap
	for _, m := range prefixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasPrefix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range suffixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasSuffix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range containsList {
		for _, pricingModel := range enableAbilities {
			if strings.Contains(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}

	// 预加载供应商
	var vendors []Vendor
	_ = DB.Find(&vendors).Error
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	// 构建对前端友好的供应商列表
	nextVendors := make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		nextVendors = append(nextVendors, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	modelGroupsMap := make(map[string]*types.Set[string])

	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = types.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
	}

	//这里使用切片而不是Set，因为一个模型可能支持多个端点类型，并且第一个端点是优先使用端点
	modelSupportEndpointsStr := make(map[string][]string)

	// 先根据已有能力填充原生端点
	for _, ability := range enableAbilities {
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
		for _, channelType := range channelTypes {
			if !common.StringsContains(endpoints, string(channelType)) {
				endpoints = append(endpoints, string(channelType))
			}
		}
		modelSupportEndpointsStr[ability.Model] = endpoints
	}

	// 再补充模型自定义端点：若配置有效则替换默认端点，不做合并
	for modelName, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			endpoints := make([]string, 0, len(raw))
			for k, v := range raw {
				switch v.(type) {
				case string, map[string]interface{}:
					if !common.StringsContains(endpoints, k) {
						endpoints = append(endpoints, k)
					}
				}
			}
			if len(endpoints) > 0 {
				modelSupportEndpointsStr[modelName] = endpoints
			}
		}
	}

	nextSupportedEndpointTypes := make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		nextSupportedEndpointTypes[model] = supportedEndpoints
	}

	// 构建全局 supportedEndpointMap（默认 + 自定义覆盖）
	nextSupportedEndpoints := make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range nextSupportedEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := nextSupportedEndpoints[string(et)]; !exists {
					nextSupportedEndpoints[string(et)] = info
				}
			}
		}
	}
	// 2. 自定义端点（models 表）覆盖默认
	for _, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					nextSupportedEndpoints[k] = common.EndpointInfo{Path: val, Method: "POST"}
				case map[string]interface{}:
					ep := common.EndpointInfo{Method: "POST"}
					if p, ok := val["path"].(string); ok {
						ep.Path = p
					}
					if m, ok := val["method"].(string); ok {
						ep.Method = strings.ToUpper(m)
					}
					nextSupportedEndpoints[k] = ep
				default:
					// ignore unsupported types
				}
			}
		}
	}

	nextPricing := make([]Pricing, 0)
	for model, groups := range modelGroupsMap {
		pricing := Pricing{
			ModelName:              model,
			EnableGroup:            groups.Items(),
			SupportedEndpointTypes: nextSupportedEndpointTypes[model],
		}

		// 补充模型元数据（描述、标签、供应商、状态）
		if meta, ok := metaMap[model]; ok {
			// 若模型被禁用(status!=1)，则直接跳过，不返回给前端
			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
		}
		modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			modelRatio, _, _ := ratio_setting.GetModelRatio(model)
			pricing.ModelRatio = modelRatio
			pricing.CompletionRatio = ratio_setting.GetCompletionRatio(model)
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := ratio_setting.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := ratio_setting.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if ratio_setting.ContainsAudioRatio(model) {
			audioRatio := ratio_setting.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if ratio_setting.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		if billingMode := billing_setting.GetBillingMode(model); billingMode == "tiered_expr" {
			if expr, ok := billing_setting.GetBillingExpr(model); ok && strings.TrimSpace(expr) != "" {
				pricing.BillingMode = billingMode
				pricing.BillingExpr = expr
			}
		}
		nextPricing = append(nextPricing, pricing)
	}

	// 防止大更新后数据不通用
	if len(nextPricing) > 0 {
		nextPricing[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 将定价、供应商、端点与派生索引作为一个不可变快照一次发布，
	// 避免读者观察到刷新过程中的半成品状态。
	nextModelEnableGroups := make(map[string][]string, len(nextPricing))
	nextModelQuotaTypes := make(map[string]int, len(nextPricing))
	for _, p := range nextPricing {
		nextModelEnableGroups[p.ModelName] = p.EnableGroup
		nextModelQuotaTypes[p.ModelName] = p.QuotaType
	}

	pricingCacheLock.Lock()
	pricingCache = pricingCacheSnapshot{
		pricing:                nextPricing,
		vendors:                nextVendors,
		supportedEndpointTypes: nextSupportedEndpointTypes,
		supportedEndpoints:     nextSupportedEndpoints,
		modelEnableGroups:      nextModelEnableGroups,
		modelQuotaTypes:        nextModelQuotaTypes,
		lastSuccessfulRefresh:  time.Now(),
	}
	pricingCacheLock.Unlock()
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	pricingCacheLock.RLock()
	defer pricingCacheLock.RUnlock()
	if pricingCache.supportedEndpoints == nil {
		return nil
	}
	result := make(map[string]common.EndpointInfo, len(pricingCache.supportedEndpoints))
	for endpoint, info := range pricingCache.supportedEndpoints {
		result[endpoint] = info
	}
	return result
}
