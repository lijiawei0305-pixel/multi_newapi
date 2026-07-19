package model

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaDataBucketKeyIsNormalizedAndUnambiguous(t *testing.T) {
	base := QuotaData{
		UserID: 7, Username: "ab", ModelName: "c", CreatedAt: 3600,
		UseGroup: "default", TokenID: 8, ChannelID: 9, NodeName: "node",
		Count: 1, Quota: 2, TokenUsed: 3,
	}
	normalized := normalizeQuotaDataBucket(base)
	assert.Len(t, normalized.BucketKey, 64)

	differentMetrics := base
	differentMetrics.Count = 99
	differentMetrics.Quota = 100
	differentMetrics.TokenUsed = 101
	assert.Equal(t, normalized.BucketKey, normalizeQuotaDataBucket(differentMetrics).BucketKey)

	ambiguousWithoutLengths := base
	ambiguousWithoutLengths.Username = "a"
	ambiguousWithoutLengths.ModelName = "bc"
	assert.NotEqual(t, normalized.BucketKey, normalizeQuotaDataBucket(ambiguousWithoutLengths).BucketKey)

	withEmbeddedSeparators := base
	withEmbeddedSeparators.Username = "ab\x00c"
	withEmbeddedSeparators.ModelName = ""
	assert.NotEqual(t, normalized.BucketKey, normalizeQuotaDataBucket(withEmbeddedSeparators).BucketKey)

	sharedPrefix := strings.Repeat("界", 64)
	longA := base
	longA.Username = sharedPrefix + "甲"
	longB := base
	longB.Username = sharedPrefix + "乙"
	normalizedA := normalizeQuotaDataBucket(longA)
	normalizedB := normalizeQuotaDataBucket(longB)
	assert.Equal(t, sharedPrefix, normalizedA.Username)
	assert.Equal(t, normalizedA.BucketKey, normalizedB.BucketKey, "the key must follow the final persisted varchar(64) value")
}

func TestQuotaDataBucketMigrationRequiresOfflineCutoverForExistingTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quota-existing-empty.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&QuotaData{}))

	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "")
	err = ensureQuotaDataBucketUniqueIndex(db, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop every old-version application node")
	assert.Contains(t, err.Error(), "exactly one new-version migration node")
	assert.False(t, db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex))

	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "1")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(db, true))
	assert.True(t, db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex))
	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(db, true))
}

func TestQuotaDataBucketMigrationNormalizesAndMergesLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quota-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE quota_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bucket_key VARCHAR(64) NULL,
		user_id INTEGER NULL,
		username VARCHAR(255) NULL,
		model_name VARCHAR(255) NULL,
		created_at BIGINT NULL,
		use_group VARCHAR(255) NULL,
		token_id INTEGER NULL,
		channel_id INTEGER NULL,
		node_name VARCHAR(255) NULL,
		token_used INTEGER NULL,
		count INTEGER NULL,
		quota INTEGER NULL
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO quota_data
		(bucket_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, token_used, count, quota)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 4, 2, 3)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO quota_data
		(bucket_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, token_used, count, quota)
		VALUES ('', 0, '', '', 0, '', 0, 0, '', 11, 5, 7)`).Error)
	sharedPrefix := strings.Repeat("界", 64)
	for i, suffix := range []string{"甲", "乙"} {
		require.NoError(t, db.Exec(`INSERT INTO quota_data
			(bucket_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, token_used, count, quota)
			VALUES ('', 1, ?, 'model', 3600, 'default', 2, 3, 'node', ?, ?, ?)`,
			sharedPrefix+suffix, i+1, i+2, i+3).Error)
	}

	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "1")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(db, true))
	require.True(t, db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex))
	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(db, true), "the completed migration must not require a persistent acknowledgement")

	var rows []QuotaData
	require.NoError(t, db.Order("user_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, 7, rows[0].Count)
	assert.Equal(t, 10, rows[0].Quota)
	assert.Equal(t, 15, rows[0].TokenUsed)
	assert.NotEmpty(t, rows[0].BucketKey)
	assert.Equal(t, sharedPrefix, rows[1].Username)
	assert.Equal(t, 5, rows[1].Count)
	assert.Equal(t, 7, rows[1].Quota)
	assert.Equal(t, 3, rows[1].TokenUsed)
	assert.NotEmpty(t, rows[1].BucketKey)

	duplicate := rows[0]
	duplicate.Id = 0
	require.Error(t, db.Create(&duplicate).Error, "the migrated bucket key must be unique")
	require.NoError(t, upsertQuotaData(db, QuotaData{
		UserID: rows[0].UserID, Username: rows[0].Username, ModelName: rows[0].ModelName,
		CreatedAt: rows[0].CreatedAt, UseGroup: rows[0].UseGroup, TokenID: rows[0].TokenID,
		ChannelID: rows[0].ChannelID, NodeName: rows[0].NodeName, Count: 1, Quota: 2, TokenUsed: 3,
	}))
	require.NoError(t, db.Where("bucket_key = ?", rows[0].BucketKey).First(&rows[0]).Error)
	assert.Equal(t, 8, rows[0].Count)
	assert.Equal(t, 12, rows[0].Quota)
	assert.Equal(t, 18, rows[0].TokenUsed)
}

func TestBillingProjectionQuotaDataConcurrentFirstBucketIsAtomic(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "quota-concurrent.db") + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(db, false))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := range workers {
		i := i
		go func() {
			ready.Done()
			<-start
			errs <- db.Transaction(func(tx *gorm.DB) error {
				return applyBillingProjectionQuotaDataTx(tx, &BillingProjectionOutbox{
					ProjectionKey: "quota-concurrent-" + string(rune('a'+i)), QuotaDataEnabled: true,
					LogUserId: 71, LogUsername: "projection-user", LogModelName: "projection-model",
					LogCreatedAt: 7201, LogGroup: "default", LogTokenId: 72, LogChannelId: 73,
					QuotaDataNodeName: "projection-node", LogQuota: i + 1, QuotaDataTokenUsed: i,
				})
			})
		}()
	}
	ready.Wait()
	close(start)
	for range workers {
		require.NoError(t, <-errs)
	}
	close(errs)

	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, workers, rows[0].Count)
	assert.Equal(t, workers*(workers+1)/2, rows[0].Quota)
	assert.Equal(t, workers*(workers-1)/2, rows[0].TokenUsed)
}
