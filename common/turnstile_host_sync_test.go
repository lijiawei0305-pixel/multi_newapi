package common_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/stretchr/testify/assert"
)

func TestTurnstileMainHostsStayInSyncWithTenantBaseDomain(t *testing.T) {
	t.Setenv(common.EnvTurnstileExtraHosts, "")

	assert.True(t, common.TurnstileAppliesToHost(tenant.BaseDomain))
	assert.True(t, common.TurnstileAppliesToHost("www."+tenant.BaseDomain))
}
