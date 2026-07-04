package reportrepo

// 覆盖钱包消耗台账（mt_wallet_consume_log）的读侧聚合：WalletConsumption（scope 求和）与
// WalletConsumptionByTenant（per-tenant，跨租户 <>0）。这是财务报表 v3「钱包消耗」精确口径的数据源。

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

func seedWalletConsume(t *testing.T, db *gorm.DB, tenantID, userID, walletQuota int64, requestID string, ts time.Time) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO mt_wallet_consume_log (tenant_id, user_id, wallet_quota, request_id, created_at) VALUES (?,?,?,?,?)`,
		tenantID, userID, walletQuota, requestID, ts,
	).Error; err != nil {
		t.Fatalf("seed wallet consume (tenant=%d req=%s): %v", tenantID, requestID, err)
	}
}

// TestWalletConsumption_ScopedAndByTenant 断言：区间过滤生效、单租户 scope 精确、跨租户(nil)排除
// tenant_id=0（未归属主站），per-tenant 聚合同样排除 tenant_id=0，各租户互不串号。
func TestWalletConsumption_ScopedAndByTenant(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()

	// 租户 5：3000 + 2000（区间内，两笔）；租户 9：5000（区间内）。
	seedWalletConsume(t, db, 5, 100, 3000, "r1", base)
	seedWalletConsume(t, db, 5, 101, 2000, "r2", base.Add(time.Minute))
	seedWalletConsume(t, db, 9, 102, 5000, "r3", base)
	// 区间外：不计入。
	seedWalletConsume(t, db, 5, 103, 999, "r-out", base.Add(48*time.Hour))
	// 未归属（tenant_id=0）：跨租户聚合与 per-tenant 聚合都须排除。
	seedWalletConsume(t, db, 0, 104, 7777, "r-zero", base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()

	// 单租户 scope：租户 5 = 5000（3000+2000，排除区间外 999）。
	tid5 := int64(5)
	if q, err := repo.WalletConsumption(ctx, &tid5, start, end); err != nil {
		t.Fatalf("WalletConsumption(5): %v", err)
	} else if q != 5000 {
		t.Fatalf("WalletConsumption(5) = %d, want 5000", q)
	}

	// 跨租户 scope（nil）：5000(租户5) + 5000(租户9) = 10000，排除 tenant_id=0 的 7777 与区间外 999。
	if q, err := repo.WalletConsumption(ctx, nil, start, end); err != nil {
		t.Fatalf("WalletConsumption(nil): %v", err)
	} else if q != 10000 {
		t.Fatalf("WalletConsumption(nil) = %d, want 10000 (excludes tenant_id=0 and out-of-range)", q)
	}

	// per-tenant：{5:5000, 9:5000}，不含 tenant_id=0。
	m, err := repo.WalletConsumptionByTenant(ctx, start, end)
	if err != nil {
		t.Fatalf("WalletConsumptionByTenant: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("WalletConsumptionByTenant len = %d, want 2; map=%v", len(m), m)
	}
	if m[5] != 5000 {
		t.Fatalf("byTenant[5] = %d, want 5000", m[5])
	}
	if m[9] != 5000 {
		t.Fatalf("byTenant[9] = %d, want 5000", m[9])
	}
	if _, ok := m[0]; ok {
		t.Fatalf("byTenant must exclude tenant_id=0, got entry %d", m[0])
	}
}
