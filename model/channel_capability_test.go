package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestClaudeChannelSelectionSkipsUnsupportedHigherPriority(t *testing.T) {
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

	db, err := gorm.Open(sqlite.Open("file:claude-capability?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))

	unsupportedPriority := int64(100)
	supportedPriority := int64(10)
	unsupportedWeight := uint(1000)
	supportedWeight := uint(0)
	channels := []Channel{
		{
			Id: 1, Type: constant.ChannelTypeBaidu, Key: "unsupported", Name: "unsupported-baidu",
			Status: common.ChannelStatusEnabled, Models: "claude-capability-test", Group: "default",
			Priority: &unsupportedPriority, Weight: &unsupportedWeight,
		},
		{
			Id: 2, Type: constant.ChannelTypeAnthropic, Key: "supported", Name: "supported-anthropic",
			Status: common.ChannelStatusEnabled, Models: "claude-capability-test", Group: "default",
			Priority: &supportedPriority, Weight: &supportedWeight,
		},
	}
	require.NoError(t, DB.Create(&channels).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "claude-capability-test", ChannelId: 1, Enabled: true, Priority: &unsupportedPriority, Weight: unsupportedWeight},
		{Group: "default", Model: "claude-capability-test", ChannelId: 2, Enabled: true, Priority: &supportedPriority, Weight: supportedWeight},
	}).Error)

	common.MemoryCacheEnabled = false
	selected, err := GetRandomSatisfiedChannel("default", "claude-capability-test", 0, "/v1/messages")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2, selected.Id, "DB routing must skip the unsupported higher-priority adaptor")

	selected, err = GetRandomSatisfiedChannel("default", "claude-capability-test", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1, selected.Id, "the capability is endpoint-specific, not a global channel disable")

	common.MemoryCacheEnabled = true
	InitChannelCache()
	selected, err = GetRandomSatisfiedChannel("default", "claude-capability-test", 0, "/v1/messages")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2, selected.Id, "cached routing must enforce the same capability contract")
}
