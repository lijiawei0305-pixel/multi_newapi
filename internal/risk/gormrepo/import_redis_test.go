package gormrepo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/risk"
)

func TestParseRiskKey(t *testing.T) {
	scope, ck, isPlan, err := parseRiskKey("risk:trial:user:42")
	require.NoError(t, err)
	require.Equal(t, risk.ScopeTrialUser, scope)
	require.Equal(t, "42", ck)
	require.False(t, isPlan)

	scope, ck, isPlan, err = parseRiskKey("risk:trial:device:ip-1.2.3.4")
	require.NoError(t, err)
	require.Equal(t, risk.ScopeTrialDevice, scope)
	require.Equal(t, "ip-1.2.3.4", ck)

	scope, ck, isPlan, err = parseRiskKey("risk:purchase:9:user:7")
	require.NoError(t, err)
	require.Equal(t, risk.ScopePlan, scope)
	require.Equal(t, "9:7", ck)
	require.True(t, isPlan)

	_, _, _, err = parseRiskKey("other:key")
	require.Error(t, err)
}
