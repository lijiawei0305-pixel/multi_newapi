package mtwire

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type migrationSQLRecorder struct {
	statements []string
}

func (r *migrationSQLRecorder) LogMode(logger.LogLevel) logger.Interface      { return r }
func (r *migrationSQLRecorder) Info(context.Context, string, ...interface{})  {}
func (r *migrationSQLRecorder) Warn(context.Context, string, ...interface{})  {}
func (r *migrationSQLRecorder) Error(context.Context, string, ...interface{}) {}
func (r *migrationSQLRecorder) Trace(_ context.Context, _ time.Time, sqlFn func() (string, int64), _ error) {
	statement, _ := sqlFn()
	r.statements = append(r.statements, strings.Join(strings.Fields(statement), " "))
}

func newMigrationDryRunDB(t *testing.T, dialect string) (*gorm.DB, *migrationSQLRecorder) {
	t.Helper()
	connection, err := sql.Open(sqlite.DriverName, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	var dialector gorm.Dialector
	switch dialect {
	case "sqlite":
		dialector = &sqlite.Dialector{Conn: connection}
	case "mysql":
		dialector = mysql.New(mysql.Config{Conn: connection, SkipInitializeWithVersion: true})
	case "postgres":
		dialector = postgres.New(postgres.Config{Conn: connection, WithoutReturning: true})
	default:
		t.Fatalf("unsupported test dialect %q", dialect)
	}

	recorder := &migrationSQLRecorder{}
	db, err := gorm.Open(dialector, &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               recorder,
	})
	require.NoError(t, err)
	return db, recorder
}

func TestAgentMigrationDDLByDialect(t *testing.T) {
	tests := []struct {
		name          string
		addColumnSQL  string
		createIdxSQL  string
		dropColumnSQL string
	}{
		{
			name:          "sqlite",
			addColumnSQL:  "ALTER TABLE `users` ADD `tenant_id` integer NOT NULL DEFAULT 0",
			createIdxSQL:  "CREATE INDEX `idx_users_tenant` ON `users`(`tenant_id`)",
			dropColumnSQL: `ALTER TABLE "agent_profiles" DROP COLUMN "type"`,
		},
		{
			name:          "mysql",
			addColumnSQL:  "ALTER TABLE `users` ADD `tenant_id` bigint NOT NULL DEFAULT 0",
			createIdxSQL:  "CREATE INDEX `idx_users_tenant` ON `users`(`tenant_id`)",
			dropColumnSQL: "ALTER TABLE `agent_profiles` DROP COLUMN `type`",
		},
		{
			name:          "postgres",
			addColumnSQL:  `ALTER TABLE "users" ADD "tenant_id" bigint NOT NULL DEFAULT 0`,
			createIdxSQL:  `CREATE INDEX IF NOT EXISTS "idx_users_tenant" ON "users" ("tenant_id")`,
			dropColumnSQL: `ALTER TABLE "agent_profiles" DROP COLUMN "type"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, recorder := newMigrationDryRunDB(t, tt.name)
			columns := &usersOwnershipColumns{}
			require.NoError(t, db.Migrator().AddColumn(columns, "TenantID"))
			require.NoError(t, db.Migrator().CreateIndex(columns, "idx_users_tenant"))
			require.NoError(t, dropLegacyAgentProfileTypeColumn(db, &legacyAgentProfileColumns{}))

			generated := strings.Join(recorder.statements, "\n")
			assert.Contains(t, generated, tt.addColumnSQL)
			assert.Contains(t, generated, tt.createIdxSQL)
			assert.Contains(t, generated, tt.dropColumnSQL)
		})
	}
}

func TestAppMigrateSQLiteCompletes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.UserSubscription{}))
	require.NoError(t, db.Exec(`CREATE TABLE agent_profiles (
		tenant_id INTEGER PRIMARY KEY,
		level INTEGER NOT NULL DEFAULT 0,
		type TEXT NOT NULL DEFAULT ''
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO agent_profiles (tenant_id, level, type) VALUES (7, 0, 'legacy')`).Error)

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})

	app := &App{DB: db}
	require.NoError(t, app.Migrate())
	assert.True(t, db.Migrator().HasColumn(&usersOwnershipColumns{}, "TenantID"))
	assert.True(t, db.Migrator().HasColumn(&usersOwnershipColumns{}, "PromotionChannelID"))
	assert.True(t, db.Migrator().HasIndex(&usersOwnershipColumns{}, "idx_users_tenant"))
	assert.True(t, db.Migrator().HasIndex(&usersOwnershipColumns{}, "idx_users_promotion_channel"))
	assert.False(t, db.Migrator().HasColumn(&legacyAgentProfileColumns{}, "Type"))

	var level int
	require.NoError(t, db.Table("agent_profiles").Select("level").Where("tenant_id = ?", 7).Scan(&level).Error)
	assert.Equal(t, 1, level)

	require.NoError(t, db.Table("agent_profiles").Where("tenant_id = ?", 7).Update("level", 3).Error)
	require.NoError(t, migrateUsersTenantID(db))
	require.NoError(t, migrateUsersPromotionChannelID(db))
	require.NoError(t, migrateAgentProfilesDropType(db))
	require.NoError(t, db.Table("agent_profiles").Select("level").Where("tenant_id = ?", 7).Scan(&level).Error)
	assert.Equal(t, 3, level, "a completed one-time migration must not reset later agent levels")
}
