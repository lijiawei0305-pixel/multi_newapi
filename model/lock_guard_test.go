package model

// 守卫测试：钉死「行级锁必须真的发出 SELECT … FOR UPDATE」这一整类问题。
//
// 背景（2026-07-17 增量审计 · Critical）：GORM v1 的 Set("gorm:query_" + "option",
// "FOR UPDATE") 约定在 GORM v2（本仓 gorm.io/gorm v1.25.x）中已被移除并**静默忽略**
// ——不报错、不告警，查询照常返回但不加任何锁。全仓曾有 17 处这种「假锁」，
// 紧随其后的「读取→计算→写回」在并发下构成丢失更新（订阅计量/充值/兑换码）。
// 修复统一收敛到 lockForUpdate（model/lock.go），本文件三层钉死：
//   1. lockForUpdate 在 MySQL 方言下必须生成 FOR UPDATE；
//   2. 老写法确实被 GORM v2 静默忽略（记录根因，防止有人"改回去"）；
//   3. 全仓 *.go 封禁老写法字面量（CI/本地 go test 即拦截回潮）。

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// legacyQueryOptionKey 用拼接构造，避免本文件自身命中下方的全仓字面量封禁。
const legacyQueryOptionKey = "gorm:query_" + "option"

// newMySQLDryRunDB 构造一个 MySQL 方言的 DryRun 连接：只生成 SQL、不发任何网络请求
// （SkipInitializeWithVersion 免掉 SELECT VERSION()，DisableAutomaticPing 免掉 Ping）。
func newMySQLDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "guard:guard@tcp(127.0.0.1:1)/guard?charset=utf8mb4",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	return db
}

func TestLockForUpdateEmitsForUpdateOnMySQL(t *testing.T) {
	db := newMySQLDryRunDB(t)

	var subs []UserSubscription
	stmt := lockForUpdate(db).Where("id = ?", 1).Find(&subs).Statement
	require.Contains(t, stmt.SQL.String(), " FOR UPDATE",
		"lockForUpdate 在 MySQL 方言下必须生成 FOR UPDATE 行锁")

	var user User
	stmt = lockForUpdate(db.Session(&gorm.Session{DryRun: true})).First(&user, 1).Statement
	require.Contains(t, stmt.SQL.String(), " FOR UPDATE",
		"lockForUpdate 对 First 同样必须生成 FOR UPDATE 行锁")
}

func TestLegacyQueryOptionIsSilentlyIgnoredByGormV2(t *testing.T) {
	db := newMySQLDryRunDB(t)

	var subs []UserSubscription
	stmt := db.Set(legacyQueryOptionKey, "FOR UPDATE").Where("id = ?", 1).Find(&subs).Statement
	require.NotContains(t, stmt.SQL.String(), "FOR UPDATE",
		"GORM v2 若开始支持 v1 的 query_option 写法，此断言会失败——届时再评估是否放开封禁；"+
			"在那之前该写法等于没加锁，必须走 lockForUpdate")
}

func TestLockForUpdateSkipsSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UserSubscription{}))

	// SQLite 无行锁语义，lockForUpdate 显式跳过：SQL 里不得出现 FOR UPDATE，真执行不报错。
	var subs []UserSubscription
	stmt := lockForUpdate(db.Session(&gorm.Session{DryRun: true})).Where("id = ?", 1).Find(&subs).Statement
	require.NotContains(t, stmt.SQL.String(), "FOR UPDATE")
	require.NoError(t, lockForUpdate(db).Where("id = ?", 1).Find(&subs).Error)
}

func TestNoLegacyQueryOptionLiteralInRepo(t *testing.T) {
	banned := []byte(legacyQueryOptionKey)
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Dir(filepath.Dir(thisFile))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("未找到仓库根（go.mod）: %v", err)
	}

	var hits []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "web", "bulb-orbit", "doc", "docs", "logs", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if !bytes.Contains(data, banned) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if bytes.Contains(line, banned) {
				hits = append(hits, fmt.Sprintf("%s:%d", rel, i+1))
			}
		}
		return nil
	}))
	require.Emptyf(t, hits,
		"发现 GORM v1 假锁写法 Set(%q)——GORM v2 会静默忽略它（等于没加锁），一律改用 lockForUpdate(tx)：\n%s",
		legacyQueryOptionKey, strings.Join(hits, "\n"))
}
