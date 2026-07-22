package model

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*dto.AdvancedCustomConfig
var channelSyncLock sync.RWMutex

type channelCacheRuntimeState struct {
	lock         sync.Mutex
	pollingIndex int
}

// channelRuntimeStates holds the only mutable request-time channel-cache state.
// The map is protected by channelSyncLock; each cursor is protected by its own
// lock so polling different channels never contends on a global write lock.
var channelRuntimeStates map[int]*channelCacheRuntimeState

// cloneChannelInfo returns a detached cache snapshot. ChannelInfo contains maps,
// so copying the struct alone would still let callers mutate cache-owned state.
func cloneChannelInfo(info ChannelInfo) ChannelInfo {
	info.MultiKeyStatusList = maps.Clone(info.MultiKeyStatusList)
	info.MultiKeyDisabledReason = maps.Clone(info.MultiKeyDisabledReason)
	info.MultiKeyDisabledTime = maps.Clone(info.MultiKeyDisabledTime)
	return info
}

// cloneChannel returns a fully detached channel snapshot. Cached channels are
// never exposed directly: callers may freely enrich or mutate the returned
// channel without racing with cache refreshes or changing future selections.
func cloneChannel(channel *Channel) *Channel {
	if channel == nil {
		return nil
	}

	cloned := *channel
	cloned.ChannelInfo = cloneChannelInfo(channel.ChannelInfo)
	cloned.Keys = append([]string(nil), channel.Keys...)
	if channel.OpenAIOrganization != nil {
		value := *channel.OpenAIOrganization
		cloned.OpenAIOrganization = &value
	}
	if channel.TestModel != nil {
		value := *channel.TestModel
		cloned.TestModel = &value
	}
	if channel.Weight != nil {
		value := *channel.Weight
		cloned.Weight = &value
	}
	if channel.BaseURL != nil {
		value := *channel.BaseURL
		cloned.BaseURL = &value
	}
	if channel.ModelMapping != nil {
		value := *channel.ModelMapping
		cloned.ModelMapping = &value
	}
	if channel.StatusCodeMapping != nil {
		value := *channel.StatusCodeMapping
		cloned.StatusCodeMapping = &value
	}
	if channel.Priority != nil {
		value := *channel.Priority
		cloned.Priority = &value
	}
	if channel.AutoBan != nil {
		value := *channel.AutoBan
		cloned.AutoBan = &value
	}
	if channel.Tag != nil {
		value := *channel.Tag
		cloned.Tag = &value
	}
	if channel.Setting != nil {
		value := *channel.Setting
		cloned.Setting = &value
	}
	if channel.ParamOverride != nil {
		value := *channel.ParamOverride
		cloned.ParamOverride = &value
	}
	if channel.HeaderOverride != nil {
		value := *channel.HeaderOverride
		cloned.HeaderOverride = &value
	}
	if channel.Remark != nil {
		value := *channel.Remark
		cloned.Remark = &value
	}
	return &cloned
}

// cloneCachedChannelLocked overlays mutable runtime state on an immutable
// channel snapshot. The caller must hold channelSyncLock for reading or writing.
func cloneCachedChannelLocked(id int, channel *Channel) *Channel {
	cloned := cloneChannel(channel)
	if cloned == nil || cloned.ChannelInfo.MultiKeyMode != constant.MultiKeyModePolling {
		return cloned
	}
	state := channelRuntimeStates[id]
	if state == nil {
		return cloned
	}
	state.lock.Lock()
	cloned.ChannelInfo.MultiKeyPollingIndex = state.pollingIndex
	state.lock.Unlock()
	return cloned
}

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*dto.AdvancedCustomConfig)
	var channels []*Channel
	DB.Find(&channels)
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = config
			}
		}
	}
	var abilities []*Ability
	DB.Find(&abilities)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.Split(channel.Group, ",")
		for _, group := range groups {
			models := strings.Split(channel.Models, ",")
			for _, model := range models {
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	newChannelRuntimeStates := make(map[int]*channelCacheRuntimeState)
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				state := &channelCacheRuntimeState{pollingIndex: channel.ChannelInfo.MultiKeyPollingIndex}
				if oldChannel, ok := channelsIDM[i]; ok &&
					oldChannel.ChannelInfo.IsMultiKey &&
					oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling &&
					channelRuntimeStates[i] != nil {
					// Preserve the live cursor across a database refresh.
					state = channelRuntimeStates[i]
					channel.ChannelInfo.MultiKeyPollingIndex = state.pollingIndex
				}
				newChannelRuntimeStates[i] = state
			}
		}
	}
	channelsIDM = newChannelId2channel
	channelRuntimeStates = newChannelRuntimeStates
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func GetRandomSatisfiedChannel(group string, model string, retry int, requestPath string) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannel(group, model, retry, requestPath)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels := filterChannelsByRequestPath(group2model2channels[group][model], requestPath)

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels = filterChannelsByRequestPath(group2model2channels[group][normalizedModel], requestPath)
	}

	if len(channels) == 0 {
		return nil, nil
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return cloneCachedChannelLocked(channels[0], channel), nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	uniquePriorities := make(map[int]bool)
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// get the priority for the given retry number
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if channel.GetPriority() == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return cloneCachedChannelLocked(channel.Id, channel), nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

// filterChannelsByRequestPath restricts candidates by endpoint capability.
// Advanced Custom channels must declare a matching route, and Anthropic
// Messages requests exclude adaptors without Claude conversion support.
// When requestPath is empty (non-relay callers) filtering is skipped.
// Caller must hold channelSyncLock (read lock). The cached slice is never mutated.
func filterChannelsByRequestPath(channels []int, requestPath string) []int {
	if requestPath == "" || len(channels) == 0 {
		return channels
	}
	filtered := make([]int, 0, len(channels))
	for _, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			// keep it so the downstream consistency error is raised as before
			filtered = append(filtered, channelId)
			continue
		}
		if constant.IsClaudeMessagesPath(requestPath) && !constant.ChannelTypeSupportsClaudeMessages(channel.Type) {
			continue
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			filtered = append(filtered, channelId)
			continue
		}
		if config := channel2advancedCustomConfig[channelId]; config != nil && config.SupportsPath(requestPath) {
			filtered = append(filtered, channelId)
		}
	}
	return filtered
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return cloneCachedChannelLocked(id, c), nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	info := cloneCachedChannelLocked(id, c).ChannelInfo
	return &info, nil
}

// cacheGetNextEnabledKey selects from the current cache snapshot and advances a
// polling channel's index under channelSyncLock. No mutable cache pointer leaves
// this function, so request code does not need to retain a cache lock afterward.
func cacheGetNextEnabledKey(id int) (string, int, *types.NewAPIError) {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	channel, ok := channelsIDM[id]
	if !ok {
		return "", 0, types.NewError(
			fmt.Errorf("渠道# %d，已不存在", id),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	keys := channel.GetKeys()
	if len(keys) == 0 {
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	enabledIndexes := make([]int, 0, len(keys))
	for index := range keys {
		status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
		if !exists || status == common.ChannelStatusEnabled {
			enabledIndexes = append(enabledIndexes, index)
		}
	}
	if len(enabledIndexes) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		selectedIndex := enabledIndexes[rand.Intn(len(enabledIndexes))]
		return keys[selectedIndex], selectedIndex, nil
	case constant.MultiKeyModePolling:
		state := channelRuntimeStates[id]
		if state == nil {
			return "", 0, types.NewError(
				errors.New("channel polling state is unavailable"),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			)
		}
		state.lock.Lock()
		defer state.lock.Unlock()

		start := state.pollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for offset := 0; offset < len(keys); offset++ {
			selectedIndex := (start + offset) % len(keys)
			status, exists := channel.ChannelInfo.MultiKeyStatusList[selectedIndex]
			if exists && status != common.ChannelStatusEnabled {
				continue
			}

			state.pollingIndex = (selectedIndex + 1) % len(keys)
			return keys[selectedIndex], selectedIndex, nil
		}
	}

	selectedIndex := enabledIndexes[0]
	return keys[selectedIndex], selectedIndex, nil
}

// cacheUpdateMultiKeyStatus applies a key-status transition while holding the
// same lock that protects polling state. The cached channel is replaced rather
// than mutated, preserving immutable snapshots for concurrent readers.
func cacheUpdateMultiKeyStatus(id int, usingKey string, status int, reason string) bool {
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()

	channel, ok := channelsIDM[id]
	if !ok {
		return false
	}
	updated := cloneChannel(channel)
	previousStatus := updated.Status
	handlerMultiKeyUpdate(updated, usingKey, status, reason)
	channelsIDM[id] = updated
	if previousStatus != updated.Status {
		cacheUpdateChannelStatusLocked(id, updated.Status)
	}
	return true
}

func cacheUpdateChannelStatusLocked(id int, status int) {
	if channel, ok := channelsIDM[id]; ok {
		updated := cloneChannel(channel)
		updated.Status = status
		channelsIDM[id] = updated
	}
	if status == common.ChannelStatusEnabled {
		return
	}

	// Delete disabled channels from the routing index.
	for group, model2channels := range group2model2channels {
		for model, channels := range model2channels {
			for i, channelId := range channels {
				if channelId == id {
					group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
					break
				}
			}
		}
	}
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	cacheUpdateChannelStatusLocked(id, status)
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel == nil {
		return
	}

	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	if channelRuntimeStates == nil {
		channelRuntimeStates = make(map[int]*channelCacheRuntimeState)
	}
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		logger.LogDebug(context.Background(), "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
	}
	cloned := cloneChannel(channel)
	channelsIDM[channel.Id] = cloned
	if cloned.ChannelInfo.IsMultiKey && cloned.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		channelRuntimeStates[channel.Id] = &channelCacheRuntimeState{
			pollingIndex: cloned.ChannelInfo.MultiKeyPollingIndex,
		}
	} else {
		delete(channelRuntimeStates, channel.Id)
	}
	logger.LogDebug(context.Background(), "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)
}
