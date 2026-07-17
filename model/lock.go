package model

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockForUpdate 给当前查询加真实的行级锁（SELECT … FOR UPDATE）。
//
// 必须走本助手，严禁 GORM v1 的 query_option 老写法：本仓 gorm.io/gorm v1.25.x
// 属 GORM v2，v2 已移除该设置项并**静默忽略**——查询照常返回但不加任何锁，
// 随后的「读取→计算→写回」在并发下即丢失更新（订阅计量、充值、兑换码 17 处
// 全踩过，见 model/lock_guard_test.go 与 CLAUDE.md C8）。
// SQLite 方言显式跳过：SQLite 本无行锁语义（写事务全库互斥）；glebarez 方言即便收到
// Locking 子句也会静默剥除（实测 err=nil）——显式跳过不依赖方言的剥除行为，并把
// 「SQLite 下测不到锁」写成明面事实：并发正确性回归必须上 MySQL 栈。
func lockForUpdate(tx *gorm.DB) *gorm.DB {
	if strings.HasPrefix(tx.Dialector.Name(), "sqlite") {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}
