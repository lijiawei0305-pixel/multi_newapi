package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSQLiteDSNWithSafeTransactions(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{name: "plain path", dsn: "one-api.db"},
		{name: "existing query", dsn: "file:one-api.db?cache=shared"},
		{name: "legacy unsupported timeout", dsn: "one-api.db?_busy_timeout=30000"},
		{name: "custom valid timeout", dsn: "one-api.db?_pragma=busy_timeout(7500)"},
		{name: "existing immediate lock", dsn: "one-api.db?_txlock=immediate"},
		{name: "unsafe deferred lock", dsn: "one-api.db?_txlock=deferred"},
		{name: "safe exclusive lock", dsn: "one-api.db?_txlock=exclusive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := sqliteDSNWithSafeTransactions(test.dsn)
			lowerResult := strings.ToLower(result)
			assert.Contains(t, lowerResult, "_pragma=busy_timeout")
			assert.NotContains(t, lowerResult, "_txlock=deferred")
			assert.True(t, strings.Contains(lowerResult, "_txlock=immediate") || strings.Contains(lowerResult, "_txlock=exclusive"))
		})
	}
}
