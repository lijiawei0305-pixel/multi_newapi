package model

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	BucketKey string `json:"-" gorm:"type:varchar(64);not null;default:''"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup  string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID   int    `json:"token_id" gorm:"index;default:0"`
	ChannelID int    `json:"channel_id" gorm:"index;default:0"`
	NodeName  string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

const quotaDataBucketUniqueIndex = "idx_quota_data_bucket_key"

// quotaDataBucketKeyEncoder is the versioned, unambiguous byte contract for a
// quota-data bucket identity. Integers are fixed-width signed values encoded as
// two's-complement big endian; strings are UTF-8 bytes prefixed by an unsigned
// 64-bit byte length.
type quotaDataBucketKeyEncoder []byte

func (e *quotaDataBucketKeyEncoder) appendInt64(value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	*e = append(*e, encoded[:]...)
}

func (e *quotaDataBucketKeyEncoder) appendString(value string) {
	e.appendInt64(int64(len([]byte(value))))
	*e = append(*e, []byte(value)...)
}

func quotaDataBucketKey(quotaData QuotaData) string {
	encoded := quotaDataBucketKeyEncoder{}
	encoded.appendString("quota-data-bucket-v1")
	encoded.appendInt64(int64(quotaData.UserID))
	encoded.appendString(quotaData.Username)
	encoded.appendString(quotaData.ModelName)
	encoded.appendInt64(quotaData.CreatedAt)
	encoded.appendString(quotaData.UseGroup)
	encoded.appendInt64(int64(quotaData.TokenID))
	encoded.appendInt64(int64(quotaData.ChannelID))
	encoded.appendString(quotaData.NodeName)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// normalizeQuotaDataBucket must be applied before both hashing and persistence
// so SQLite, MySQL, and PostgreSQL derive the same identity from the same final
// varchar(64) dimension values.
func normalizeQuotaDataBucket(quotaData QuotaData) QuotaData {
	quotaData.Username = truncateBillingProjectionField(quotaData.Username, 64)
	quotaData.ModelName = truncateBillingProjectionField(quotaData.ModelName, 64)
	quotaData.UseGroup = truncateBillingProjectionField(quotaData.UseGroup, 64)
	quotaData.NodeName = truncateBillingProjectionField(quotaData.NodeName, 64)
	quotaData.BucketKey = quotaDataBucketKey(quotaData)
	return quotaData
}

func (q *QuotaData) BeforeCreate(_ *gorm.DB) error {
	normalized := normalizeQuotaDataBucket(*q)
	*q = normalized
	return nil
}

func upsertQuotaData(tx *gorm.DB, quotaData QuotaData) error {
	if tx == nil {
		return fmt.Errorf("quota data transaction is nil")
	}
	quotaData = normalizeQuotaDataBucket(quotaData)
	quotaData.Id = 0
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "bucket_key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"count": gorm.Expr("? + ?",
				clause.Column{Table: clause.CurrentTable, Name: "count"}, quotaData.Count),
			"quota": gorm.Expr("? + ?",
				clause.Column{Table: clause.CurrentTable, Name: "quota"}, quotaData.Quota),
			"token_used": gorm.Expr("? + ?",
				clause.Column{Table: clause.CurrentTable, Name: "token_used"}, quotaData.TokenUsed),
		}),
	}).Create(&quotaData).Error
}

// ensureQuotaDataBucketUniqueIndex upgrades the legacy aggregate table without
// asking AutoMigrate to create a unique index over duplicate historical rows.
// Operators must drain old-version application nodes before this migration:
// old binaries do not populate bucket_key and cannot safely write after the
// unique index is installed.
func ensureQuotaDataBucketUniqueIndex(db *gorm.DB, legacyTable bool) error {
	if db == nil {
		return fmt.Errorf("quota data migration database is nil")
	}
	if !db.Migrator().HasTable(&QuotaData{}) {
		return fmt.Errorf("quota data table is missing")
	}
	if !db.Migrator().HasColumn(&QuotaData{}, "BucketKey") {
		return fmt.Errorf("quota data bucket_key column is missing")
	}
	if db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex) {
		return nil
	}
	if legacyTable && !common.GetEnvOrDefaultBool("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", false) {
		return errors.New("quota_data requires a one-time offline bucket-key migration: stop every old-version application node, back up the database, set QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED=1 on exactly one new-version migration node, start that node and wait for the unique index to finish, then remove the acknowledgement and start the remaining new-version nodes")
	}

	var lastIndexError error
	for range 3 {
		if err := backfillQuotaDataBucketKeysAndMerge(db); err != nil {
			return err
		}
		if db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex) {
			return nil
		}
		lastIndexError = db.Exec(
			"CREATE UNIQUE INDEX " + quotaDataBucketUniqueIndex + " ON quota_data (bucket_key)",
		).Error
		if lastIndexError == nil || db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketUniqueIndex) {
			return nil
		}
	}
	return fmt.Errorf(
		"create quota data bucket unique index after duplicate cleanup: %w; drain all old-version nodes that do not write bucket_key before retrying",
		lastIndexError,
	)
}

func backfillQuotaDataBucketKeysAndMerge(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// Historical schemas allowed NULL in dimensions and counters. Normalize
		// them before scanning so every database maps the same row to the same key.
		if err := tx.Exec(`UPDATE quota_data SET
			bucket_key = COALESCE(bucket_key, ''),
			user_id = COALESCE(user_id, 0),
			username = COALESCE(username, ''),
			model_name = COALESCE(model_name, ''),
			created_at = COALESCE(created_at, 0),
			use_group = COALESCE(use_group, ''),
			token_id = COALESCE(token_id, 0),
			channel_id = COALESCE(channel_id, 0),
			node_name = COALESCE(node_name, ''),
			token_used = COALESCE(token_used, 0),
			count = COALESCE(count, 0),
			quota = COALESCE(quota, 0)`).Error; err != nil {
			return err
		}

		lastID := 0
		for {
			var rows []QuotaData
			if err := tx.Where("id > ?", lastID).Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for i := range rows {
				lastID = rows[i].Id
				normalized := normalizeQuotaDataBucket(rows[i])
				if normalized.BucketKey == rows[i].BucketKey && normalized.Username == rows[i].Username &&
					normalized.ModelName == rows[i].ModelName && normalized.UseGroup == rows[i].UseGroup &&
					normalized.NodeName == rows[i].NodeName {
					continue
				}
				if err := tx.Model(&QuotaData{}).Where("id = ?", rows[i].Id).Updates(map[string]interface{}{
					"bucket_key": normalized.BucketKey,
					"username":   normalized.Username,
					"model_name": normalized.ModelName,
					"use_group":  normalized.UseGroup,
					"node_name":  normalized.NodeName,
				}).Error; err != nil {
					return err
				}
			}
		}

		type duplicateBucket struct {
			BucketKey      string
			DuplicateCount int64
		}
		var duplicates []duplicateBucket
		if err := tx.Model(&QuotaData{}).
			Select("bucket_key, COUNT(*) AS duplicate_count").
			Group("bucket_key").Having("COUNT(*) > 1").Find(&duplicates).Error; err != nil {
			return err
		}
		for _, duplicate := range duplicates {
			var rows []QuotaData
			query := tx.Where("bucket_key = ?", duplicate.BucketKey).Order("id ASC")
			if tx.Dialector.Name() != "sqlite" {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) < 2 {
				continue
			}
			canonical := normalizeQuotaDataBucket(rows[0])
			var count, quota, tokenUsed int64
			duplicateIDs := make([]int, 0, len(rows)-1)
			for i := range rows {
				normalized := normalizeQuotaDataBucket(rows[i])
				if normalized.UserID != canonical.UserID || normalized.Username != canonical.Username ||
					normalized.ModelName != canonical.ModelName || normalized.CreatedAt != canonical.CreatedAt ||
					normalized.UseGroup != canonical.UseGroup || normalized.TokenID != canonical.TokenID ||
					normalized.ChannelID != canonical.ChannelID || normalized.NodeName != canonical.NodeName {
					return fmt.Errorf("quota data bucket key collision for %s", duplicate.BucketKey)
				}
				count += int64(rows[i].Count)
				quota += int64(rows[i].Quota)
				tokenUsed += int64(rows[i].TokenUsed)
				if i > 0 {
					duplicateIDs = append(duplicateIDs, rows[i].Id)
				}
			}
			if int64(int(count)) != count || int64(int(quota)) != quota || int64(int(tokenUsed)) != tokenUsed {
				return fmt.Errorf("quota data aggregate overflow for bucket %s", duplicate.BucketKey)
			}
			if err := tx.Model(&QuotaData{}).Where("id = ?", canonical.Id).Updates(map[string]interface{}{
				"count": int(count), "quota": int(quota), "token_used": int(tokenUsed),
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", duplicateIDs).Delete(&QuotaData{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	UseGroup  string
	TokenID   int
	ChannelID int
	NodeName  string
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}
var quotaDataFlushLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	normalized := normalizeQuotaDataBucket(*quotaData)
	quotaData = &normalized
	key := quotaData.BucketKey
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	quotaDataFlushLock.Lock()
	defer quotaDataFlushLock.Unlock()

	CacheQuotaDataLock.Lock()
	quotaDataBatch := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	size := len(quotaDataBatch)
	failedQuotaData := make([]*QuotaData, 0)
	saved := 0
	// Every flush is one atomic insert-or-increment keyed by the normalized
	// bucket identity. Failed items are merged back into the live cache below.
	for _, quotaData := range quotaDataBatch {
		err := upsertQuotaData(DB, *quotaData)
		if err != nil {
			failedQuotaData = append(failedQuotaData, quotaData)
			common.SysLog(fmt.Sprintf("saveQuotaData error: %s", err))
			continue
		}
		saved++
	}

	if len(failedQuotaData) > 0 {
		CacheQuotaDataLock.Lock()
		for _, quotaData := range failedQuotaData {
			logQuotaDataCache(quotaData)
		}
		CacheQuotaDataLock.Unlock()
	}
	common.SysLog(fmt.Sprintf("保存数据看板数据完成，共%d条，成功%d条，待重试%d条", size, saved, len(failedQuotaData)))
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
