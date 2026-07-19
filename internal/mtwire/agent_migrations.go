package mtwire

import "gorm.io/gorm"

// usersOwnershipColumns is deliberately separate from model.User: these are
// multitenant-owned columns on the native users table, so upstream rebases do
// not need to carry mtwire fields in the core model. GORM still gets a real
// schema here, which lets its migrator emit valid SQLite/MySQL/PostgreSQL DDL.
type usersOwnershipColumns struct {
	TenantID           int64 `gorm:"column:tenant_id;not null;default:0;index:idx_users_tenant"`
	PromotionChannelID int64 `gorm:"column:promotion_channel_id;not null;default:0;index:idx_users_promotion_channel"`
}

func (usersOwnershipColumns) TableName() string { return "users" }

// legacyAgentProfileColumns describes only the removed column. It is not used
// by normal reads or writes; it gives the non-SQLite GORM migrators enough
// schema information to quote the table and column for their dialect.
type legacyAgentProfileColumns struct {
	Type string `gorm:"column:type"`
}

func (legacyAgentProfileColumns) TableName() string { return "agent_profiles" }

// migrateUsersOwnershipColumn adds one mtwire-owned users column and its index.
// Keeping the index as a separate step makes the migration restart-safe when a
// previous startup added the column but failed before creating the index.
func migrateUsersOwnershipColumn(db *gorm.DB, field, index string) error {
	columns := &usersOwnershipColumns{}
	if !db.Migrator().HasColumn(columns, field) {
		if err := db.Migrator().AddColumn(columns, field); err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(columns, index) {
		if err := db.Migrator().CreateIndex(columns, index); err != nil {
			return err
		}
	}
	return nil
}

// migrateUsersTenantID idempotently adds users.tenant_id and its lookup index
// without modifying the native model.User struct.
func migrateUsersTenantID(db *gorm.DB) error {
	return migrateUsersOwnershipColumn(db, "TenantID", "idx_users_tenant")
}

// migrateUsersPromotionChannelID idempotently adds
// users.promotion_channel_id and its lookup index without modifying the native
// model.User struct.
func migrateUsersPromotionChannelID(db *gorm.DB) error {
	return migrateUsersOwnershipColumn(db, "PromotionChannelID", "idx_users_promotion_channel")
}

func dropLegacyAgentProfileTypeColumn(db *gorm.DB, legacy *legacyAgentProfileColumns) error {
	if db.Dialector.Name() == "sqlite" {
		// glebarez/sqlite's GORM migrator rebuilds the whole table for
		// DropColumn. Its DDL parser cannot reliably parse the decimal(20,8)
		// columns in agent_profiles, while the bundled SQLite supports native
		// DROP COLUMN. Native DDL avoids that lossy rebuild path.
		return db.Exec(`ALTER TABLE "agent_profiles" DROP COLUMN "type"`).Error
	}
	return db.Migrator().DropColumn(legacy, "Type")
}

// migrateAgentProfilesDropType promotes existing agents to the independent
// tier and removes the obsolete type column exactly once. Column existence is
// the durable migration marker, so later startups never reset level again.
func migrateAgentProfilesDropType(db *gorm.DB) error {
	legacy := &legacyAgentProfileColumns{}
	if !db.Migrator().HasColumn(legacy, "Type") {
		return nil
	}
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).
		Table(legacy.TableName()).
		UpdateColumn("level", 1).Error; err != nil {
		return err
	}
	return dropLegacyAgentProfileTypeColumn(db, legacy)
}
