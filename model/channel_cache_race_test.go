package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func installChannelCacheForTest(t *testing.T, channels ...*Channel) {
	t.Helper()

	previousMemoryCache := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	previousGroups := group2model2channels
	previousChannels := channelsIDM
	previousAdvancedConfigs := channel2advancedCustomConfig
	previousRuntimeStates := channelRuntimeStates
	group2model2channels = map[string]map[string][]int{
		"default": {"cache-race-model": {}},
	}
	channelsIDM = make(map[int]*Channel, len(channels))
	channelRuntimeStates = make(map[int]*channelCacheRuntimeState, len(channels))
	for _, channel := range channels {
		channelsIDM[channel.Id] = cloneChannel(channel)
		if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			channelRuntimeStates[channel.Id] = &channelCacheRuntimeState{
				pollingIndex: channel.ChannelInfo.MultiKeyPollingIndex,
			}
		}
		if channel.Status == common.ChannelStatusEnabled {
			group2model2channels["default"]["cache-race-model"] = append(
				group2model2channels["default"]["cache-race-model"],
				channel.Id,
			)
		}
	}
	channel2advancedCustomConfig = make(map[int]*dto.AdvancedCustomConfig)
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true

	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		channelSyncLock.Lock()
		group2model2channels = previousGroups
		channelsIDM = previousChannels
		channel2advancedCustomConfig = previousAdvancedConfigs
		channelRuntimeStates = previousRuntimeStates
		channelSyncLock.Unlock()
	})
}

func TestChannelCacheReturnsDetachedSnapshots(t *testing.T) {
	organization := "organization"
	testModel := "test-model"
	weight := uint(50)
	baseURL := "https://example.com"
	modelMapping := "model-mapping"
	statusCodeMapping := "status-code-mapping"
	priority := int64(10)
	autoBan := 1
	tag := "tag"
	setting := "setting"
	paramOverride := "param-override"
	headerOverride := "header-override"
	remark := "remark"
	installChannelCacheForTest(t, &Channel{
		Id:                 41,
		Key:                "key-1\nkey-2",
		OpenAIOrganization: &organization,
		TestModel:          &testModel,
		Status:             common.ChannelStatusEnabled,
		Name:               "cached-channel",
		Weight:             &weight,
		BaseURL:            &baseURL,
		Models:             "cache-race-model",
		Group:              "default",
		ModelMapping:       &modelMapping,
		StatusCodeMapping:  &statusCodeMapping,
		Priority:           &priority,
		AutoBan:            &autoBan,
		Tag:                &tag,
		Setting:            &setting,
		ParamOverride:      &paramOverride,
		HeaderOverride:     &headerOverride,
		Remark:             &remark,
		Keys:               []string{"key-1", "key-2"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeyStatusList:     map[int]int{1: common.ChannelStatusManuallyDisabled},
			MultiKeyDisabledReason: map[int]string{1: "reason"},
			MultiKeyDisabledTime:   map[int]int64{1: 123},
			MultiKeyMode:           constant.MultiKeyModePolling,
		},
	})

	first, err := CacheGetChannel(41)
	require.NoError(t, err)
	second, err := CacheGetChannel(41)
	require.NoError(t, err)

	assert.NotSame(t, first, second)
	assert.NotSame(t, first.OpenAIOrganization, second.OpenAIOrganization)
	assert.NotSame(t, first.TestModel, second.TestModel)
	assert.NotSame(t, first.Weight, second.Weight)
	assert.NotSame(t, first.BaseURL, second.BaseURL)
	assert.NotSame(t, first.ModelMapping, second.ModelMapping)
	assert.NotSame(t, first.StatusCodeMapping, second.StatusCodeMapping)
	assert.NotSame(t, first.Priority, second.Priority)
	assert.NotSame(t, first.AutoBan, second.AutoBan)
	assert.NotSame(t, first.Tag, second.Tag)
	assert.NotSame(t, first.Setting, second.Setting)
	assert.NotSame(t, first.ParamOverride, second.ParamOverride)
	assert.NotSame(t, first.HeaderOverride, second.HeaderOverride)
	assert.NotSame(t, first.Remark, second.Remark)

	first.Name = "mutated"
	*first.BaseURL = "https://mutated.example.com"
	*first.Weight = 999
	first.Keys[0] = "mutated-key"
	first.ChannelInfo.MultiKeyPollingIndex = 99
	first.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled
	first.ChannelInfo.MultiKeyDisabledReason[1] = "mutated-reason"
	first.ChannelInfo.MultiKeyDisabledTime[1] = 999

	afterMutation, err := CacheGetChannel(41)
	require.NoError(t, err)
	assert.Equal(t, "cached-channel", afterMutation.Name)
	assert.Equal(t, "https://example.com", *afterMutation.BaseURL)
	assert.Equal(t, uint(50), *afterMutation.Weight)
	assert.Equal(t, []string{"key-1", "key-2"}, afterMutation.Keys)
	assert.Equal(t, 0, afterMutation.ChannelInfo.MultiKeyPollingIndex)
	assert.NotContains(t, afterMutation.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, "reason", afterMutation.ChannelInfo.MultiKeyDisabledReason[1])
	assert.Equal(t, int64(123), afterMutation.ChannelInfo.MultiKeyDisabledTime[1])

	info, err := CacheGetChannelInfo(41)
	require.NoError(t, err)
	info.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled
	info.MultiKeyPollingIndex = 77
	unchangedInfo, err := CacheGetChannelInfo(41)
	require.NoError(t, err)
	assert.NotContains(t, unchangedInfo.MultiKeyStatusList, 0)
	assert.Equal(t, 0, unchangedInfo.MultiKeyPollingIndex)
}

func TestRandomSatisfiedChannelReturnsDetachedSnapshot(t *testing.T) {
	priority := int64(1)
	weight := uint(1)
	installChannelCacheForTest(t, &Channel{
		Id:       42,
		Key:      "key",
		Status:   common.ChannelStatusEnabled,
		Name:     "selected-channel",
		Weight:   &weight,
		Models:   "cache-race-model",
		Group:    "default",
		Priority: &priority,
		ChannelInfo: ChannelInfo{
			MultiKeyStatusList: map[int]int{},
		},
	})

	selected, err := GetRandomSatisfiedChannel("default", "cache-race-model", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	selected.Name = "mutated"
	selected.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled

	selectedAgain, err := GetRandomSatisfiedChannel("default", "cache-race-model", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selectedAgain)
	assert.Equal(t, "selected-channel", selectedAgain.Name)
	assert.Empty(t, selectedAgain.ChannelInfo.MultiKeyStatusList)
}

func TestCachedMultiKeyPollingAdvancesAtomically(t *testing.T) {
	installChannelCacheForTest(t, &Channel{
		Id:     43,
		Key:    "key-1\nkey-2\nkey-3",
		Status: common.ChannelStatusEnabled,
		Keys:   []string{"key-1", "key-2", "key-3"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyStatusList:   map[int]int{1: common.ChannelStatusManuallyDisabled},
			MultiKeyPollingIndex: 0,
			MultiKeyMode:         constant.MultiKeyModePolling,
		},
	})

	snapshot, err := CacheGetChannel(43)
	require.NoError(t, err)
	for _, expected := range []struct {
		key   string
		index int
	}{{"key-1", 0}, {"key-3", 2}, {"key-1", 0}} {
		key, index, apiErr := snapshot.GetNextEnabledKey()
		require.Nil(t, apiErr)
		assert.Equal(t, expected.key, key)
		assert.Equal(t, expected.index, index)
		// Mutating an old request snapshot must not reset the shared cursor.
		snapshot.ChannelInfo.MultiKeyPollingIndex = 0
	}

	info, err := CacheGetChannelInfo(43)
	require.NoError(t, err)
	assert.Equal(t, 1, info.MultiKeyPollingIndex)
}

func TestChannelCacheRefreshPreservesLivePollingCursor(t *testing.T) {
	previousDB := DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousDatabaseType := common.MainDatabaseType()
	channelSyncLock.Lock()
	previousGroups := group2model2channels
	previousChannels := channelsIDM
	previousAdvancedConfigs := channel2advancedCustomConfig
	previousRuntimeStates := channelRuntimeStates
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.SetMainDatabaseType(previousDatabaseType)
		channelSyncLock.Lock()
		group2model2channels = previousGroups
		channelsIDM = previousChannels
		channel2advancedCustomConfig = previousAdvancedConfigs
		channelRuntimeStates = previousRuntimeStates
		channelSyncLock.Unlock()
	})

	db, err := gorm.Open(sqlite.Open("file:channel-cache-refresh?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))

	priority := int64(1)
	weight := uint(1)
	channel := Channel{
		Id:       46,
		Key:      "key-1\nkey-2",
		Status:   common.ChannelStatusEnabled,
		Name:     "before-refresh",
		Models:   "cache-race-model",
		Group:    "default",
		Priority: &priority,
		Weight:   &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "cache-race-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	InitChannelCache()
	snapshot, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, index, apiErr := snapshot.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, 0, index)

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("name", "after-refresh").Error)
	InitChannelCache()
	refreshed, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "after-refresh", refreshed.Name)
	assert.Equal(t, 1, refreshed.ChannelInfo.MultiKeyPollingIndex)
	key, index, apiErr = refreshed.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-2", key)
	assert.Equal(t, 1, index)
}

func TestCachedMultiKeyStatusUpdateReplacesSnapshotAndAffectsSelection(t *testing.T) {
	installChannelCacheForTest(t, &Channel{
		Id:     45,
		Key:    "key-1\nkey-2",
		Status: common.ChannelStatusEnabled,
		Keys:   []string{"key-1", "key-2"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeyStatusList:     map[int]int{},
			MultiKeyDisabledReason: map[int]string{},
			MultiKeyDisabledTime:   map[int]int64{},
			MultiKeyMode:           constant.MultiKeyModePolling,
		},
	})

	oldSnapshot, err := CacheGetChannel(45)
	require.NoError(t, err)
	require.True(t, cacheUpdateMultiKeyStatus(
		45,
		"key-1",
		common.ChannelStatusManuallyDisabled,
		"upstream rejected key",
	))
	assert.Empty(t, oldSnapshot.ChannelInfo.MultiKeyStatusList, "published snapshots stay immutable")

	disabledSnapshot, err := CacheGetChannel(45)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, disabledSnapshot.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "upstream rejected key", disabledSnapshot.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Positive(t, disabledSnapshot.ChannelInfo.MultiKeyDisabledTime[0])
	key, index, apiErr := disabledSnapshot.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-2", key)
	assert.Equal(t, 1, index)

	require.True(t, cacheUpdateMultiKeyStatus(45, "key-1", common.ChannelStatusEnabled, "recovered"))
	enabledSnapshot, err := CacheGetChannel(45)
	require.NoError(t, err)
	assert.NotContains(t, enabledSnapshot.ChannelInfo.MultiKeyStatusList, 0)
	key, index, apiErr = enabledSnapshot.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, 0, index)
}

func TestChannelCacheConcurrentSnapshotsPollingAndUpdates(t *testing.T) {
	baseURL := "https://example.com"
	installChannelCacheForTest(t, &Channel{
		Id:      44,
		Key:     "key-1\nkey-2\nkey-3",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		Keys:    []string{"key-1", "key-2", "key-3"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeyStatusList:     map[int]int{},
			MultiKeyDisabledReason: map[int]string{},
			MultiKeyDisabledTime:   map[int]int64{},
			MultiKeyMode:           constant.MultiKeyModePolling,
		},
	})

	start := make(chan struct{})
	errorsFound := make(chan error, 16)
	var workers sync.WaitGroup

	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 200; iteration++ {
				snapshot, err := CacheGetChannel(44)
				if err != nil {
					errorsFound <- err
					return
				}
				snapshot.Name = fmt.Sprintf("worker-%d", worker)
				*snapshot.BaseURL = "https://mutated.example.com"
				snapshot.Keys[0] = "mutated-key"
				snapshot.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled
			}
		}(worker)
	}

	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 200; iteration++ {
				snapshot, err := CacheGetChannel(44)
				if err != nil {
					errorsFound <- err
					return
				}
				key, index, apiErr := snapshot.GetNextEnabledKey()
				if apiErr != nil {
					errorsFound <- apiErr
					return
				}
				if key == "" || index < 0 || index > 2 {
					errorsFound <- fmt.Errorf("invalid selection: key=%q index=%d", key, index)
					return
				}
			}
		}()
	}

	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for iteration := 0; iteration < 200; iteration++ {
			status := common.ChannelStatusManuallyDisabled
			if iteration%2 == 1 {
				status = common.ChannelStatusEnabled
			}
			if !cacheUpdateMultiKeyStatus(44, "key-2", status, "test transition") {
				errorsFound <- fmt.Errorf("cached channel disappeared")
				return
			}
		}
	}()

	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for iteration := 0; iteration < 200; iteration++ {
			inputURL := "https://example.com"
			input := &Channel{
				Id:      44,
				Key:     "key-1\nkey-2\nkey-3",
				Status:  common.ChannelStatusEnabled,
				BaseURL: &inputURL,
				Keys:    []string{"key-1", "key-2", "key-3"},
				ChannelInfo: ChannelInfo{
					IsMultiKey:           true,
					MultiKeyStatusList:   map[int]int{},
					MultiKeyPollingIndex: iteration % 3,
					MultiKeyMode:         constant.MultiKeyModePolling,
				},
			}
			CacheUpdateChannel(input)
			// The cache must not retain aliases to caller-owned update objects.
			*input.BaseURL = "https://mutated.example.com"
			input.Keys[0] = "mutated-key"
			input.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled
		}
	}()

	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}

	finalSnapshot, err := CacheGetChannel(44)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", *finalSnapshot.BaseURL)
	assert.Equal(t, []string{"key-1", "key-2", "key-3"}, finalSnapshot.Keys)
}
