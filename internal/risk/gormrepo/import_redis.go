package gormrepo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/risk"
)

// ImportResult 是 Redis→DB 幂等导入的计数摘要（不删任何 Redis 键）。
type ImportResult struct {
	Scanned  int // SCAN 到的相关键数
	Imported int // 新写入台账行数
	Skipped  int // 已存在/非法键跳过
	Errors   int // 单键解析/写库失败数（汇总后仍返回 error 若 >0 且严格模式）
}

// ImportFromRedis 将现网 risk:trial:* / risk:purchase:* 键幂等写入 risk_purchase_claims。
// 不删除、不修改 Redis 键；已存在 (scope,claim_key) 则跳过。
// 用于 C4 台账上线：历史占用不丢，Redis 之后可被重建而不丢权威。
func (r *Repo) ImportFromRedis(ctx context.Context, rdb *redis.Client) (ImportResult, error) {
	var out ImportResult
	if rdb == nil {
		return out, nil
	}
	now := time.Now().Unix()
	patterns := []string{"risk:trial:*", "risk:purchase:*"}
	for _, pattern := range patterns {
		var cursor uint64
		for {
			keys, next, err := rdb.Scan(ctx, cursor, pattern, 200).Result()
			if err != nil {
				return out, fmt.Errorf("redis SCAN %s: %w", pattern, err)
			}
			for _, key := range keys {
				out.Scanned++
				imp, skip, ierr := r.importOneKey(ctx, rdb, key, now)
				if ierr != nil {
					out.Errors++
					common.SysError(fmt.Sprintf("risk ledger import key=%s: %v", key, ierr))
					continue
				}
				if imp {
					out.Imported++
				}
				if skip {
					out.Skipped++
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return out, nil
}

func (r *Repo) importOneKey(ctx context.Context, rdb *redis.Client, key string, nowUnix int64) (imported, skipped bool, err error) {
	scope, claimKey, isPlan, err := parseRiskKey(key)
	if err != nil {
		return false, true, nil // 非目标形态：跳过不算错误
	}
	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}

	var owner int64
	count := 1
	if isPlan {
		n, perr := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if perr != nil || n <= 0 {
			return false, true, nil
		}
		count = int(n)
		// plan 键 claim_key 已是 planID:userID；owner 取 user 段
		parts := strings.SplitN(claimKey, ":", 2)
		if len(parts) == 2 {
			owner, _ = strconv.ParseInt(parts[1], 10, 64)
		}
	} else {
		owner, err = strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil || owner <= 0 {
			// 遗留 sentinel "1"：无可靠 owner，跳过以免脏写
			return false, true, nil
		}
	}

	var expiresAt int64
	if scope == risk.ScopeTrialDevice {
		ttl, terr := rdb.TTL(ctx, key).Result()
		if terr == nil && ttl > 0 {
			expiresAt = nowUnix + int64(ttl.Seconds())
		}
		// ttl<0 无过期 → expires_at=0（与终身语义一致；正常 device 应有 TTL）
	}

	row := claimRow{
		Scope:       scope,
		ClaimKey:    claimKey,
		OwnerUserID: owner,
		Count:       count,
		ExpiresAt:   expiresAt,
		CreatedAt:   nowUnix,
		UpdatedAt:   nowUnix,
	}
	// 幂等：冲突则 DoNothing
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "claim_key"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return false, false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, true, nil
	}
	return true, false, nil
}

// parseRiskKey 解析引擎键命名：
//
//	risk:trial:user:<id>
//	risk:trial:realname:<id>
//	risk:trial:device:<id>
//	risk:purchase:<planID>:user:<userID>
func parseRiskKey(key string) (scope, claimKey string, isPlan bool, err error) {
	parts := strings.Split(key, ":")
	if len(parts) < 4 || parts[0] != "risk" {
		return "", "", false, fmt.Errorf("not a risk key")
	}
	switch parts[1] {
	case "trial":
		if len(parts) < 4 {
			return "", "", false, fmt.Errorf("short trial key")
		}
		dim := parts[2]
		id := strings.Join(parts[3:], ":") // device/realname 可能含冒号
		if id == "" {
			return "", "", false, fmt.Errorf("empty claim id")
		}
		switch dim {
		case "user":
			return risk.ScopeTrialUser, id, false, nil
		case "realname":
			return risk.ScopeTrialRealname, id, false, nil
		case "device":
			return risk.ScopeTrialDevice, id, false, nil
		default:
			return "", "", false, fmt.Errorf("unknown trial dim")
		}
	case "purchase":
		// risk:purchase:<planID>:user:<userID>
		if len(parts) != 5 || parts[3] != "user" {
			return "", "", false, fmt.Errorf("bad purchase key")
		}
		planID, e1 := strconv.ParseInt(parts[2], 10, 64)
		userID, e2 := strconv.ParseInt(parts[4], 10, 64)
		if e1 != nil || e2 != nil {
			return "", "", false, fmt.Errorf("bad purchase ids")
		}
		return risk.ScopePlan, planClaimKey(planID, userID), true, nil
	default:
		return "", "", false, fmt.Errorf("unknown risk prefix")
	}
}

// ImportFromRedisIfConfigured 在 RDB 可用时执行导入并打系统日志；无 Redis 则 no-op。
func ImportFromRedisIfConfigured(ctx context.Context, db *gorm.DB, rdb *redis.Client) (ImportResult, error) {
	if db == nil || rdb == nil {
		return ImportResult{}, nil
	}
	return New(db).ImportFromRedis(ctx, rdb)
}
