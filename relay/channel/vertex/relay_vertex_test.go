package vertex

import (
	"context"
	"fmt"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRegionRejectsNonStringMappingsWithoutPanicking(t *testing.T) {
	tests := []struct {
		name   string
		config string
		model  string
		want   string
	}{
		{name: "plain region", config: "us-central1", model: "gemini", want: "us-central1"},
		{name: "model override", config: `{"gemini":"asia-east1","default":"global"}`, model: "gemini", want: "asia-east1"},
		{name: "default fallback", config: `{"default":"europe-west1"}`, model: "gemini", want: "europe-west1"},
		{name: "invalid model uses default", config: `{"gemini":7,"default":"us-west1"}`, model: "gemini", want: "us-west1"},
		{name: "invalid default uses global", config: `{"gemini":false,"default":["us-west1"]}`, model: "gemini", want: "global"},
		{name: "null mappings use global", config: `{"gemini":null,"default":null}`, model: "gemini", want: "global"},
		{name: "blank mappings use global", config: `{"gemini":" ","default":""}`, model: "gemini", want: "global"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetModelRegion(tt.config, tt.model))
		})
	}
}

func TestGetAccessTokenIgnoresCorruptCachedValue(t *testing.T) {
	const channelID = 2_147_483_640
	cacheKey := fmt.Sprintf("access-token-%d", channelID)
	Cache.DeleteIf(func(key string) bool { return key == cacheKey })
	Cache.SetDefault(cacheKey, 42)
	t.Cleanup(func() {
		Cache.DeleteIf(func(key string) bool { return key == cacheKey })
	})

	_, err := getAccessToken(context.Background(), &Adaptor{}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create signed JWT")
	_, stillCached := Cache.Dump()[cacheKey]
	assert.False(t, stillCached)
}

func TestGetAccessTokenRejectsNilContext(t *testing.T) {
	_, err := getAccessToken(nil, &Adaptor{}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is nil")
}
