// Package gormrepo 实现 risk.PurchaseLedger：表 risk_purchase_claims 为限购持久权威台账。
//
// 解决 C4 残余：终身限购若只活在 Redis，compose down -v / 无卷重建即全站归零、历史买家可再薅。
// Redis 仅作 Engine 侧加速镜像；裁决与后台释放以本表为准。
package gormrepo

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/risk"
)

// claimRow 映射 risk_purchase_claims。
// 唯一键 (scope, claim_key)：同一维度身份全局至多一行有效占用。
// 设备维 expires_at>0 且已过期时，Claim 前先删再插，语义对齐 Redis DeviceDedupTTL。
type claimRow struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Scope       string `gorm:"column:scope;type:varchar(32);not null;uniqueIndex:uk_risk_purchase_claim,priority:1"`
	ClaimKey    string `gorm:"column:claim_key;type:varchar(191);not null;uniqueIndex:uk_risk_purchase_claim,priority:2"`
	OwnerUserID int64  `gorm:"column:owner_user_id;not null;index"`
	Count       int    `gorm:"column:count;not null;default:1"` // Trial 恒 1；plan 为累计次数
	ExpiresAt   int64  `gorm:"column:expires_at;not null;default:0"` // unix 秒；0=永不过期
	CreatedAt   int64  `gorm:"column:created_at;not null;default:0"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null;default:0"`
}

func (claimRow) TableName() string { return "risk_purchase_claims" }

// Repo 是 risk.PurchaseLedger 的 GORM 实现。
type Repo struct {
	db *gorm.DB
}

// 编译期断言。
var _ risk.PurchaseLedger = (*Repo)(nil)

// New 用主库 *gorm.DB 构造。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 risk_purchase_claims。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&claimRow{})
}

// ClaimTrial 事务内多维占用；任一维唯一冲突 → ErrPurchaseLimitExceeded 并整单回滚。
func (r *Repo) ClaimTrial(ctx context.Context, userID int64, pi risk.PurchaseIdentity, deviceTTL time.Duration, now time.Time) error {
	nowUnix := now.Unix()
	type dim struct {
		scope, key string
		ttl        time.Duration
	}
	dims := []dim{{risk.ScopeTrialUser, strconv.FormatInt(userID, 10), 0}}
	if strings.TrimSpace(pi.RealNameID) != "" {
		dims = append(dims, dim{risk.ScopeTrialRealname, pi.RealNameID, 0})
	}
	if strings.TrimSpace(pi.DeviceID) != "" {
		dims = append(dims, dim{risk.ScopeTrialDevice, pi.DeviceID, deviceTTL})
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range dims {
			// 清过期行（主要是 device 维），再插入
			if err := tx.Where("scope = ? AND claim_key = ? AND expires_at > 0 AND expires_at <= ?",
				d.scope, d.key, nowUnix).Delete(&claimRow{}).Error; err != nil {
				return err
			}
			var exp int64
			if d.ttl > 0 {
				exp = now.Add(d.ttl).Unix()
			}
			row := claimRow{
				Scope:       d.scope,
				ClaimKey:    d.key,
				OwnerUserID: userID,
				Count:       1,
				ExpiresAt:   exp,
				CreatedAt:   nowUnix,
				UpdatedAt:   nowUnix,
			}
			if err := tx.Create(&row).Error; err != nil {
				if isDuplicate(err) {
					return risk.ErrPurchaseLimitExceeded
				}
				return err
			}
		}
		return nil
	})
}

// ReleaseTrial 删除台账行；共享维归属校验对齐 Engine.ReleaseTrialLimit。
func (r *Repo) ReleaseTrial(ctx context.Context, userID int64, pi risk.PurchaseIdentity, force bool) (risk.TrialReleaseResult, error) {
	res := risk.TrialReleaseResult{}
	// user 维：按 userID 建键，恒删
	if err := r.db.WithContext(ctx).Where("scope = ? AND claim_key = ?",
		risk.ScopeTrialUser, strconv.FormatInt(userID, 10)).Delete(&claimRow{}).Error; err != nil {
		return risk.TrialReleaseResult{}, err
	}
	res.Released = append(res.Released, "user")

	shared := []struct{ dim, id, scope string }{
		{"realname", pi.RealNameID, risk.ScopeTrialRealname},
		{"device", pi.DeviceID, risk.ScopeTrialDevice},
	}
	for _, s := range shared {
		if strings.TrimSpace(s.id) == "" {
			continue
		}
		var row claimRow
		err := r.db.WithContext(ctx).Where("scope = ? AND claim_key = ?", s.scope, s.id).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return risk.TrialReleaseResult{}, err
		}
		if row.OwnerUserID != userID {
			// DB 台账无遗留 "1"；另一真实用户即便 force 也拒（与 forceReleasable 精神一致）
			res.Skipped = append(res.Skipped, s.dim)
			continue
		}
		if err := r.db.WithContext(ctx).Delete(&row).Error; err != nil {
			return risk.TrialReleaseResult{}, err
		}
		res.Released = append(res.Released, s.dim)
	}
	_ = force // force 对合法跨用户占用无效；保留签名对称
	return res, nil
}

// ClaimPlan 条件自增；超限返回 ErrPurchaseLimitExceeded。
func (r *Repo) ClaimPlan(ctx context.Context, planID, userID int64, limit int) error {
	if limit <= 0 {
		return nil
	}
	key := planClaimKey(planID, userID)
	nowUnix := time.Now().Unix()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row claimRow
		q := lockForUpdate(tx).Where("scope = ? AND claim_key = ?", risk.ScopePlan, key)
		err := q.First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = claimRow{
				Scope: risk.ScopePlan, ClaimKey: key, OwnerUserID: userID,
				Count: 1, CreatedAt: nowUnix, UpdatedAt: nowUnix,
			}
			if cerr := tx.Create(&row).Error; cerr != nil {
				if isDuplicate(cerr) {
					// 并发首插冲突：重试读+增
					return r.claimPlanBump(tx, key, userID, limit, nowUnix)
				}
				return cerr
			}
			return nil
		}
		if err != nil {
			return err
		}
		if row.Count >= limit {
			return risk.ErrPurchaseLimitExceeded
		}
		return tx.Model(&claimRow{}).Where("id = ? AND count < ?", row.ID, limit).
			Updates(map[string]any{"count": row.Count + 1, "updated_at": nowUnix}).Error
	})
}

func (r *Repo) claimPlanBump(tx *gorm.DB, key string, userID int64, limit int, nowUnix int64) error {
	var row claimRow
	if err := lockForUpdate(tx).Where("scope = ? AND claim_key = ?", risk.ScopePlan, key).First(&row).Error; err != nil {
		return err
	}
	if row.Count >= limit {
		return risk.ErrPurchaseLimitExceeded
	}
	return tx.Model(&claimRow{}).Where("id = ? AND count < ?", row.ID, limit).
		Updates(map[string]any{"count": row.Count + 1, "updated_at": nowUnix, "owner_user_id": userID}).Error
}

// RollbackPlan 计数 -1；<=0 删行。
func (r *Repo) RollbackPlan(ctx context.Context, planID, userID int64) error {
	key := planClaimKey(planID, userID)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row claimRow
		err := lockForUpdate(tx).Where("scope = ? AND claim_key = ?", risk.ScopePlan, key).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Count <= 1 {
			return tx.Delete(&row).Error
		}
		return tx.Model(&row).Updates(map[string]any{
			"count":      row.Count - 1,
			"updated_at": time.Now().Unix(),
		}).Error
	})
}

// ReleasePlan 删整行。
func (r *Repo) ReleasePlan(ctx context.Context, planID, userID int64) error {
	return r.db.WithContext(ctx).Where("scope = ? AND claim_key = ?",
		risk.ScopePlan, planClaimKey(planID, userID)).Delete(&claimRow{}).Error
}

func planClaimKey(planID, userID int64) string {
	return strconv.FormatInt(planID, 10) + ":" + strconv.FormatInt(userID, 10)
}

// lockForUpdate：MySQL/PG 行锁；SQLite 无行锁语义则跳过（与 model.lockForUpdate 一致）。
func lockForUpdate(tx *gorm.DB) *gorm.DB {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return tx
	}
	// Dialector 名兜底：测试内存 sqlite 可能未设 UsingMainDatabase
	if name := tx.Dialector.Name(); name == "sqlite" || name == "sqlite3" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate")
}
