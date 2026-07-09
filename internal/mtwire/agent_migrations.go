package mtwire

import "gorm.io/gorm"

// ============================================================================
// 迁移：users.tenant_id（new-api 原生表，幂等 raw ALTER，不改 model.User struct）
// ============================================================================

// migrateUsersTenantID 幂等地给 new-api 原生 users 表加 tenant_id 列 + 索引：
// 先查 information_schema 确认无列才 ALTER（避免重复执行报错），不触碰 new-api 的 model.User
// （加字段会在 upstream rebase 时冲突，见 RETRO「原生表增列」）。下级归属读写均走轻量 Table 查询。
func migrateUsersTenantID(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'tenant_id'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil // 已有列：幂等跳过
	}
	return db.Exec(
		`ALTER TABLE users ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0,
		 ADD INDEX idx_users_tenant (tenant_id)`,
	).Error
}

// migrateUsersPromotionChannelID 幂等地给 new-api 原生 users 表加 promotion_channel_id 列 + 索引
// （= agent_promotion_channels.id；经渠道码注册的用户落此列，0 = 无渠道）。与 tenant_id 同套路：
// 先查 information_schema 确认无列才 ALTER，不触碰 new-api 的 model.User（避免 upstream rebase 冲突）。
func migrateUsersPromotionChannelID(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'promotion_channel_id'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil // 已有列：幂等跳过
	}
	return db.Exec(
		`ALTER TABLE users ADD COLUMN promotion_channel_id BIGINT NOT NULL DEFAULT 0,
		 ADD INDEX idx_users_promotion_channel (promotion_channel_id)`,
	).Error
}

// migrateAgentProfilesDropType 一次性破坏性迁移：现有代理全部升为独立档（level=1）后删除废弃的 type 列。
// 幂等：以 type 列是否仍存在为一次性信号——列已删即跳过，绝不重复回填（避免每次启动重置 level）。
// 与 migrateUsersTenantID 同套路：information_schema 守卫的 raw MySQL；本地 sqlite 不覆盖，服务器验证。
func migrateAgentProfilesDropType(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'agent_profiles' AND column_name = 'type'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil // type 列已删：一次性迁移已执行，幂等跳过
	}
	// 回填：现有代理全部升为独立档（不拉黑已有站点，spec §3 迁移）。仅当 type 列尚存时执行，故只跑一次。
	if err := db.Exec(`UPDATE agent_profiles SET level = 1`).Error; err != nil {
		return err
	}
	return db.Exec(`ALTER TABLE agent_profiles DROP COLUMN type`).Error
}
