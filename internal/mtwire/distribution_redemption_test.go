package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	walletrepo "github.com/QuantumNous/new-api/internal/wallet/gormrepo"
)

// newRedemptionApp 装配 App：sqlite + 原生 users(id,tenant_id,quota) + walletrepo（含
// agent_redemption_codes）。供 HandleAgentCreateRedemptions 端到端跑「建码从 owner 原生 quota
// 条件预扣」的真实仓储路径（不 mock，直触付费闸门）。
func newRedemptionApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, quota INTEGER NOT NULL DEFAULT 0)").Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := walletrepo.AutoMigrate(db); err != nil {
		t.Fatalf("wallet migrate: %v", err)
	}
	return &App{DB: db, RedemptionRepo: walletrepo.New(db)}
}

func seedOwnerQuota(t *testing.T, app *App, id, tenantID, quota int64) {
	t.Helper()
	if err := app.DB.Exec("INSERT INTO users (id, tenant_id, quota) VALUES (?,?,?)", id, tenantID, quota).Error; err != nil {
		t.Fatalf("seed owner %d: %v", id, err)
	}
}

func ownerQuota(t *testing.T, app *App, id int64) int64 {
	t.Helper()
	var row struct{ Quota int64 }
	if err := app.DB.Table("users").Select("quota").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("read quota %d: %v", id, err)
	}
	return row.Quota
}

// TestHandleAgentCreateRedemptions_RejectsOverflowMint 锁死安全审计 Critical（int64 乘法溢出绕过
// 唯一付费闸门）：建码的「面额×张数」溢出不得回绕成小正数、穿过仓储层 totalQuotaUnits<=0 的付费闸门。
//
// 历史漏洞：amount_usd 只有下界。amount_usd=997121301281.5974、count=37 →
// perCode=498560650640798706（过 perCode>0 守卫）、诚实 total=1.84e19 溢出回绕成 506（过
// totalQuotaUnits<=0 守卫）→ owner 钱包实扣 506（≈$0.001）铸出 37 张 ×$997 亿的码。
//
// 修复后：面额上界（maxRedemptionAmountUSD）+ total 溢出检测双闸，请求在建码前即拒，owner quota
// 分文不动、零码落库；且界内合法请求仍照常放行（不误伤）。
func TestHandleAgentCreateRedemptions_RejectsOverflowMint(t *testing.T) {
	// 单测无 Redis：关掉 RedisEnabled 让 InvalidateUserCache 走空操作分支（同生产「未配 Redis」路径）。
	redisRestore := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = redisRestore }()

	app := newRedemptionApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	const owner = int64(500)
	const seedQuota = int64(101_500_000) // 线上正常最大用户额度（≈$203）——不足以诚实铸出任何天价码
	seedOwnerQuota(t, app, owner, tenantID, seedQuota)

	call := func(body string) apiResp {
		c, rec := newAgentCtx(tenantID, "POST", body, nil)
		c.Set("id", int(owner)) // AgentOwnerAuth 注入的 owner id（handler 读 c.GetInt("id")）
		app.HandleAgentCreateRedemptions(c)
		return decodeResp(t, rec)
	}

	// 1) 报告中的精确利用载荷：溢出回绕后 total≈506 —— 必须被拒（AGENT_INPUT_INVALID），绝不铸码。
	if r := call(`{"amount_usd":997121301281.5974,"count":37}`); r.Success || r.Code != "AGENT_INPUT_INVALID" {
		t.Fatalf("overflow-mint exploit must be rejected, got %+v", r)
	}
	// 2) 刚过面额上界（$1,000,000.01 × 1 张）：拒（上界主闸）。
	if r := call(`{"amount_usd":1000000.01,"count":1}`); r.Success || r.Code != "AGENT_INPUT_INVALID" {
		t.Fatalf("over-cap amount must be rejected, got %+v", r)
	}
	// 关键不变式：两次被拒后 owner quota 分文未动、零码落库（付费闸门未被穿过）。
	if q := ownerQuota(t, app, owner); q != seedQuota {
		t.Fatalf("owner quota moved on rejected mint = %d, want %d (untouched)", q, seedQuota)
	}
	if list, _ := app.RedemptionRepo.ListCodesByTenant(ctx, tenantID); len(list) != 0 {
		t.Fatalf("codes minted on reject = %d, want 0", len(list))
	}

	// 3) 合法界内请求仍照常放行：$1 × 2 张 → 扣 2×QuotaPerUnit、建 2 张码（不误伤正常业务）。
	if r := call(`{"amount_usd":1,"count":2}`); !r.Success {
		t.Fatalf("legit in-range request must succeed, got %+v", r)
	}
	spent := int64(2 * common.QuotaPerUnit)
	if q := ownerQuota(t, app, owner); q != seedQuota-spent {
		t.Fatalf("owner quota after legit spend = %d, want %d", q, seedQuota-spent)
	}
	if list, _ := app.RedemptionRepo.ListCodesByTenant(ctx, tenantID); len(list) != 2 {
		t.Fatalf("codes after legit spend = %d, want 2", len(list))
	}
}
