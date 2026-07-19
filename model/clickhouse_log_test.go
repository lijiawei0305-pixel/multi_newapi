package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsClickHouseDSN(t *testing.T) {
	cases := []struct {
		dsn  string
		want bool
	}{
		{"clickhouse://default:pass@localhost:9000/logs", true},
		{"tcp://localhost:9000/logs", true},
		{"http://localhost:8123/logs", true},
		{"https://localhost:8443/logs", true},
		{"postgres://root:pass@localhost:5432/db", false},
		{"postgresql://root:pass@localhost:5432/db", false},
		{"root:pass@tcp(localhost:3306)/db", false},
		{"local", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, isClickHouseDSN(c.dsn), "dsn=%q", c.dsn)
	}
}

func TestNormalizeClickHouseDSN(t *testing.T) {
	// https without secure gets secure=true appended and all log inserts are
	// forced synchronous for projection-key readback.
	normalized, err := normalizeClickHouseDSN("https://default:pass@localhost:8443/logs")
	require.NoError(t, err)
	assert.Contains(t, normalized, "secure=true")
	assert.Contains(t, normalized, "async_insert=0")
	assert.Contains(t, normalized, "wait_for_async_insert=1")
	assert.Contains(t, normalized, "insert_deduplicate=0")
	assert.True(t, strings.HasPrefix(normalized, "https://"))

	// Existing secure preference is preserved, while an explicit async mode is
	// overridden because it cannot satisfy immediate idempotency readback.
	normalized, err = normalizeClickHouseDSN("https://localhost:8443/logs?secure=false&async_insert=1")
	require.NoError(t, err)
	assert.Contains(t, normalized, "secure=false")
	assert.Contains(t, normalized, "async_insert=0")
	assert.Contains(t, normalized, "wait_for_async_insert=1")
	assert.Contains(t, normalized, "insert_deduplicate=0")

	for _, dsn := range []string{"clickhouse://localhost:9000/logs", "tcp://localhost:9000/logs", "http://localhost:8123/logs"} {
		normalized, err = normalizeClickHouseDSN(dsn)
		require.NoError(t, err)
		assert.Contains(t, normalized, "async_insert=0")
		assert.Contains(t, normalized, "wait_for_async_insert=1")
		assert.Contains(t, normalized, "insert_deduplicate=0")
	}
}

func TestNormalizeClickHouseDSNRejectsMultiEndpointRouting(t *testing.T) {
	tests := []string{
		"clickhouse://host-a:9000,host-b:9000/logs",
		"https://clickhouse.example/logs?alt_hosts=host-b%3A8443",
		"tcp://clickhouse.example:9000/logs?ALT_HOSTS=host-b%3A9000",
		"clickhouse://clickhouse.example:9000/logs?alt_hosts=",
	}
	for _, dsn := range tests {
		_, err := normalizeClickHouseDSN(dsn)
		require.ErrorContains(t, err, "one direct endpoint", "dsn=%q", dsn)
	}
}

func TestChooseDBRejectsClickHouseMultiEndpointLogDSN(t *testing.T) {
	t.Setenv("LOG_SQL_DSN", "clickhouse://host-a:9000,host-b:9000/logs")
	db, dbType, err := chooseDB("LOG_SQL_DSN", true)
	require.ErrorContains(t, err, "one direct endpoint")
	assert.Nil(t, db)
	assert.Empty(t, dbType)
}

func TestChooseDBRejectsClickHouseForMainDatabase(t *testing.T) {
	original, had := os.LookupEnv("SQL_DSN")
	t.Cleanup(func() {
		if had {
			require.NoError(t, os.Setenv("SQL_DSN", original))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
	require.NoError(t, os.Setenv("SQL_DSN", "clickhouse://default:pass@localhost:9000/logs"))

	db, dbType, err := chooseDB("SQL_DSN", false)
	require.Error(t, err)
	assert.Nil(t, db)
	assert.Equal(t, common.DatabaseType(""), dbType)
	assert.Contains(t, err.Error(), "does not support ClickHouse")
}

func TestClickHouseLogTTLExpression(t *testing.T) {
	assert.Equal(t, "", clickHouseLogTTLExpression(0))
	assert.Equal(t, "", clickHouseLogTTLExpression(-5))
	assert.Equal(t, "toDateTime(created_at) + INTERVAL 30 DAY DELETE", clickHouseLogTTLExpression(30))
}

func TestClickHouseLogTTLClause(t *testing.T) {
	assert.Equal(t, "", clickHouseLogTTLClause(0))
	assert.Equal(t, "\nTTL toDateTime(created_at) + INTERVAL 7 DAY DELETE", clickHouseLogTTLClause(7))
}

func TestClickHouseLogCreateTableSQL(t *testing.T) {
	withoutTTL := clickHouseLogCreateTableSQL(0, 12345)
	assert.Contains(t, withoutTTL, "CREATE TABLE IF NOT EXISTS logs")
	assert.Contains(t, withoutTTL, "ENGINE = MergeTree()")
	assert.Contains(t, withoutTTL, "PARTITION BY toYYYYMM(toDateTime(created_at))")
	assert.Contains(t, withoutTTL, "non_replicated_deduplication_window = 12345")
	assert.Contains(t, withoutTTL, "ORDER BY (created_at, request_id)")
	assert.Contains(t, withoutTTL, "projection_key Nullable(String) DEFAULT NULL")
	assert.Contains(t, withoutTTL, "INDEX idx_logs_projection_key projection_key TYPE bloom_filter(0.001) GRANULARITY 1")
	assert.NotContains(t, strings.ToUpper(withoutTTL), "UNIQUE")
	assert.NotContains(t, withoutTTL, "TTL ")

	withTTL := clickHouseLogCreateTableSQL(30, 12345)
	assert.Contains(t, withTTL, "ORDER BY (created_at, request_id)")
	assert.Contains(t, withTTL, "TTL toDateTime(created_at) + INTERVAL 30 DAY DELETE")
}

func TestClickHouseLogDeduplicationWindowFailsClosed(t *testing.T) {
	original, hadOriginal := os.LookupEnv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW")
	t.Cleanup(func() {
		if hadOriginal {
			require.NoError(t, os.Setenv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW", original))
		} else {
			require.NoError(t, os.Unsetenv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW"))
		}
	})

	require.NoError(t, os.Unsetenv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW"))
	assert.Equal(t, defaultClickHouseLogDeduplicationWindow, clickHouseLogDeduplicationWindow())
	require.NoError(t, os.Setenv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW", "0"))
	assert.Equal(t, defaultClickHouseLogDeduplicationWindow, clickHouseLogDeduplicationWindow())
	require.NoError(t, os.Setenv("LOG_SQL_CLICKHOUSE_DEDUPLICATION_WINDOW", "12345"))
	assert.Equal(t, 12345, clickHouseLogDeduplicationWindow())
}

func TestClickHouseProjectionKeyMigrationContract(t *testing.T) {
	assert.Equal(t,
		"ALTER TABLE logs ADD COLUMN IF NOT EXISTS projection_key Nullable(String) DEFAULT NULL",
		clickHouseLogProjectionKeyMigrationSQL,
	)
	assert.NotContains(t, strings.ToUpper(clickHouseLogProjectionKeyMigrationSQL), "UNIQUE")
	assert.Equal(t,
		"ALTER TABLE logs ADD INDEX IF NOT EXISTS idx_logs_projection_key projection_key TYPE bloom_filter(0.001) GRANULARITY 1",
		clickHouseLogProjectionKeyIndexMigrationSQL,
	)
	assert.NotContains(t, strings.ToUpper(clickHouseLogProjectionKeyIndexMigrationSQL), "MATERIALIZE")
}

func TestClickHouseCreateTableHasTTL(t *testing.T) {
	assert.True(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...)\nTTL toDateTime(created_at) + INTERVAL 30 DAY DELETE"))
	assert.True(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...) TTL toDateTime(created_at)"))
	assert.False(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...)\nORDER BY (created_at, request_id)"))
}

func TestClickHouseLogOrder(t *testing.T) {
	assert.Equal(t, "created_at desc, request_id desc", clickHouseLogOrder(""))
	assert.Equal(t, "logs.created_at desc, logs.request_id desc", clickHouseLogOrder("logs."))
}

func TestBuildLogLikeConditionUsesStandardEscape(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	condition, pattern, err := buildLogLikeCondition("logs.model_name", "gpt_4%")

	require.NoError(t, err)
	assert.Equal(t, "logs.model_name LIKE ? ESCAPE '!'", condition)
	assert.Equal(t, "gpt!_4%", pattern)
}

func TestBuildLogLikeConditionUsesClickHouseEscaping(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)

	condition, pattern, err := buildLogLikeCondition("logs.model_name", `gpt_4\mini%`)

	require.NoError(t, err)
	assert.Equal(t, "logs.model_name LIKE ?", condition)
	assert.Equal(t, `gpt\_4\\mini%`, pattern)
}

func TestEnsureLogRequestId(t *testing.T) {
	empty := &Log{}
	ensureLogRequestId(empty)
	assert.NotEmpty(t, empty.RequestId, "empty request id should be backfilled")

	existing := &Log{RequestId: "fixed-request-id"}
	ensureLogRequestId(existing)
	assert.Equal(t, "fixed-request-id", existing.RequestId, "existing request id must be preserved")

	assert.NotPanics(t, func() { ensureLogRequestId(nil) })
}

func TestAssignDisplayLogIds(t *testing.T) {
	logs := []*Log{{}, {}, {}}

	assignDisplayLogIds(logs, 0)
	assert.Equal(t, []int{1, 2, 3}, []int{logs[0].Id, logs[1].Id, logs[2].Id})

	assignDisplayLogIds(logs, 20)
	assert.Equal(t, []int{21, 22, 23}, []int{logs[0].Id, logs[1].Id, logs[2].Id})

	assert.NotPanics(t, func() { assignDisplayLogIds(nil, 0) })
}
