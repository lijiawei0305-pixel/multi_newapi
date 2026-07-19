package model

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPricingCacheReturnsIndependentValues(t *testing.T) {
	ratio := 0.5
	replacePricingCacheForTest(t, pricingCacheSnapshot{
		pricing: []Pricing{{
			ModelName:              "snapshot-model",
			EnableGroup:            []string{"default"},
			SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI},
			CacheRatio:             &ratio,
		}},
		vendors: []PricingVendor{{ID: 1, Name: "Snapshot Vendor"}},
		supportedEndpointTypes: map[string][]constant.EndpointType{
			"snapshot-model": {constant.EndpointTypeOpenAI},
		},
		supportedEndpoints: map[string]common.EndpointInfo{
			string(constant.EndpointTypeOpenAI): {Path: "/v1/chat/completions", Method: "POST"},
		},
		modelEnableGroups:     map[string][]string{"snapshot-model": {"default"}},
		modelQuotaTypes:       map[string]int{"snapshot-model": 0},
		lastSuccessfulRefresh: time.Now(),
	})

	pricing := GetPricing()
	pricing[0].ModelName = "mutated"
	pricing[0].EnableGroup[0] = "mutated"
	pricing[0].SupportedEndpointTypes[0] = constant.EndpointTypeGemini
	*pricing[0].CacheRatio = 99

	vendors := GetVendors()
	vendors[0].Name = "mutated"

	endpointTypes := GetModelSupportEndpointTypes("snapshot-model")
	endpointTypes[0] = constant.EndpointTypeGemini

	endpoints := GetSupportedEndpointMap()
	endpoints[string(constant.EndpointTypeOpenAI)] = common.EndpointInfo{Path: "/mutated", Method: "DELETE"}

	groups := GetModelEnableGroups("snapshot-model")
	groups[0] = "mutated"

	pricing = GetPricing()
	require.Len(t, pricing, 1)
	assert.Equal(t, "snapshot-model", pricing[0].ModelName)
	assert.Equal(t, []string{"default"}, pricing[0].EnableGroup)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, pricing[0].SupportedEndpointTypes)
	require.NotNil(t, pricing[0].CacheRatio)
	assert.Equal(t, 0.5, *pricing[0].CacheRatio)
	assert.Equal(t, "Snapshot Vendor", GetVendors()[0].Name)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, GetModelSupportEndpointTypes("snapshot-model"))
	assert.Equal(t, "/v1/chat/completions", GetSupportedEndpointMap()[string(constant.EndpointTypeOpenAI)].Path)
	assert.Equal(t, []string{"default"}, GetModelEnableGroups("snapshot-model"))
}

func TestPricingCacheConcurrentRefreshInvalidateAndRead(t *testing.T) {
	testDB, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "pricing-cache.db")+"?_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))

	vendor := Vendor{Id: 701, Name: "Race Vendor", Status: 1}
	channel := Channel{Id: 702, Type: constant.ChannelTypeOpenAI, Key: "test-key", Name: "race-channel", Status: 1}
	meta := Model{Id: 703, ModelName: "pricing-race-model", VendorID: vendor.Id, Status: 1, NameRule: NameRuleExact}
	ability := Ability{Group: "default", Model: meta.ModelName, ChannelId: channel.Id, Enabled: true}
	require.NoError(t, testDB.Create(&vendor).Error)
	require.NoError(t, testDB.Create(&channel).Error)
	require.NoError(t, testDB.Create(&meta).Error)
	require.NoError(t, testDB.Create(&ability).Error)

	originalDB := DB
	DB = testDB
	t.Cleanup(func() { DB = originalDB })
	replacePricingCacheForTest(t, pricingCacheSnapshot{})
	RefreshPricing()
	require.Len(t, GetPricing(), 1)

	start := make(chan struct{})
	errors := make(chan error, 32)
	var workers sync.WaitGroup

	for reader := 0; reader < 8; reader++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 100; iteration++ {
				pricing := GetPricing()
				if len(pricing) != 1 || pricing[0].ModelName != meta.ModelName {
					errors <- fmt.Errorf("unexpected pricing snapshot: %#v", pricing)
					return
				}
				pricing[0].EnableGroup[0] = "caller-mutation"

				vendors := GetVendors()
				if len(vendors) != 1 || vendors[0].Name != vendor.Name {
					errors <- fmt.Errorf("unexpected vendor snapshot: %#v", vendors)
					return
				}
				vendors[0].Name = "caller-mutation"

				groups := GetModelEnableGroups(meta.ModelName)
				if len(groups) != 1 || groups[0] != "default" {
					errors <- fmt.Errorf("unexpected group snapshot: %#v", groups)
					return
				}
				groups[0] = "caller-mutation"
			}
		}()
	}

	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for iteration := 0; iteration < 50; iteration++ {
			RefreshPricing()
		}
	}()

	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for iteration := 0; iteration < 50; iteration++ {
			InvalidatePricingCache()
		}
	}()

	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		assert.NoError(t, err)
	}

	pricing := GetPricing()
	require.Len(t, pricing, 1)
	assert.Equal(t, meta.ModelName, pricing[0].ModelName)
	assert.Equal(t, []string{"default"}, pricing[0].EnableGroup)
	assert.Equal(t, vendor.Name, GetVendors()[0].Name)
}

func replacePricingCacheForTest(t *testing.T, replacement pricingCacheSnapshot) {
	t.Helper()
	updatePricingLock.Lock()
	pricingCacheLock.Lock()
	original := pricingCache
	pricingCache = replacement
	pricingCacheLock.Unlock()
	updatePricingLock.Unlock()

	t.Cleanup(func() {
		updatePricingLock.Lock()
		pricingCacheLock.Lock()
		pricingCache = original
		pricingCacheLock.Unlock()
		updatePricingLock.Unlock()
	})
}
