package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTurnstileHost(t *testing.T) {
	assert.Equal(t, "www.wedreamhub.com", NormalizeTurnstileHost("WWW.WedreamHub.com:443"))
	assert.Equal(t, "test1.ccchhxx.top", NormalizeTurnstileHost("test1.ccchhxx.top."))
	assert.Equal(t, "", NormalizeTurnstileHost("   "))
}

func TestTurnstileAppliesToHost_MainSiteOnly(t *testing.T) {
	t.Setenv(EnvTurnstileExtraHosts, "")

	assert.True(t, TurnstileAppliesToHost("www.wedreamhub.com"))
	assert.True(t, TurnstileAppliesToHost("wedreamhub.com"))
	assert.True(t, TurnstileAppliesToHost("WWW.wedreamhub.com:443"))

	// Agent subdomain / OEM custom domain / local → no turnstile.
	assert.False(t, TurnstileAppliesToHost("test1.wedreamhub.com"))
	assert.False(t, TurnstileAppliesToHost("test1.ccchhxx.top"))
	assert.False(t, TurnstileAppliesToHost("localhost"))
	assert.False(t, TurnstileAppliesToHost(""))
}

func TestTurnstileAppliesToHost_ExtraHosts(t *testing.T) {
	t.Setenv(EnvTurnstileExtraHosts, "portal.example.com, login.brand.io")

	require.True(t, TurnstileAppliesToHost("portal.example.com"))
	require.True(t, TurnstileAppliesToHost("login.brand.io"))
	assert.False(t, TurnstileAppliesToHost("other.example.com"))
	assert.True(t, TurnstileAppliesToHost("www.wedreamhub.com"))
}
