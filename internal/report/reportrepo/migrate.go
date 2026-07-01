package reportrepo

// 财务报表索引迁移：在原生 logs 与代理 agent_earning_logs 上补「区间覆盖索引」，幂等可重入。
//
// 套路对齐 internal/mtwire/agent.go:49-64 migrateUsersTenantID：MySQL 先查 information_schema
// 确认索引不存在才 CREATE（CREATE INDEX 无 IF NOT EXISTS，重复执行会报错）；sqlite/pg 用
// CREATE INDEX IF NOT EXISTS 原生幂等。绝不触碰 model.Log / model.User 的 struct tag（加 tag 会在
// upstream rebase 冲突，且原生迁移 AutoMigrate 不会建这两个组合索引）。
//
// 两个索引（与 reportrepo 的热查询对齐）：
//   - agent_earning_logs(tenant_id, created_at) —— 收益 by source / 趋势 / 排行的区间扫描（主库 DB）。
//     原生 tag 仅 idx_agent_earnings_tenant=(tenant_id)，不覆盖 created_at 区间。
//   - logs(user_id, created_at) —— 消耗透镜 logs JOIN users 后按 user_id 取行 + created_at 区间（LOG_DB）。
//     原生 idx_user_id_id=(user_id, id) 不覆盖 created_at 区间。ClickHouse 日志库跳过（其 ORDER BY 主键已覆盖）。

import (
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// AutoMigrate 幂等地补两个区间覆盖索引。由 App.Migrate()（master 节点，wire.go ~228-234）在
// migrateUsersTenantID 附近链式调用。传入 db 即主库（model.DB）；日志索引落在 model.LOG_DB
// （未配独立日志库时 LOG_DB==DB，即落同库）。
func AutoMigrate(db *gorm.DB) error {
	// 收益台账区间覆盖索引（主库）。
	if err := ensureIndex(db, common.MainDatabaseType(),
		"agent_earning_logs", "idx_agent_earnings_tenant_created", "tenant_id, created_at"); err != nil {
		return err
	}

	// 原生 logs 区间覆盖索引（日志库；分库时落 LOG_DB）。
	logDB := db
	logType := common.MainDatabaseType()
	if model.LOG_DB != nil && model.DB != nil && model.LOG_DB != model.DB {
		logDB = model.LOG_DB
		logType = common.LogDatabaseType()
	}
	// ClickHouse：二级索引语义不同（按 ORDER BY 主键裁剪），不在此建普通索引。
	if logType == common.DatabaseTypeClickHouse {
		return nil
	}
	return ensureIndex(logDB, logType, "logs", "idx_logs_user_created", "user_id, created_at")
}

// ensureIndex 仅在索引缺失时建 idx ON table(cols)。MySQL 走 information_schema.statistics 守卫
// （CREATE INDEX 无 IF NOT EXISTS）；sqlite/pg 用 CREATE INDEX IF NOT EXISTS 原生幂等。
func ensureIndex(db *gorm.DB, dbType common.DatabaseType, table, idx, cols string) error {
	if db == nil {
		return nil
	}
	if dbType == common.DatabaseTypeMySQL {
		var count int64
		if err := db.Raw(
			`SELECT COUNT(*) FROM information_schema.statistics
			 WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			table, idx,
		).Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil // 已有索引：幂等跳过
		}
		return db.Exec("CREATE INDEX " + idx + " ON " + table + " (" + cols + ")").Error
	}
	// sqlite / postgres：原生幂等守卫。
	return db.Exec("CREATE INDEX IF NOT EXISTS " + idx + " ON " + table + " (" + cols + ")").Error
}
