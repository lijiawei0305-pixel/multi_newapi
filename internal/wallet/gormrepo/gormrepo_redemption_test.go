package gormrepo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/wallet"
)

// userRow 是测试用的最小原生 users 表（仅 id+quota），供 CreateCodesWithDeduction 触原生预扣。
type userRow struct {
	ID    int64 `gorm:"column:id;primaryKey"`
	Quota int64 `gorm:"column:quota;not null;default:0"`
}

func (userRow) TableName() string { return "users" }

func newRedemptionTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // :memory: 每连接独立库
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.AutoMigrate(&userRow{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	return New(db)
}

func seedUserQuota(t *testing.T, r *Repo, userID, quota int64) {
	t.Helper()
	if err := r.db.Create(&userRow{ID: userID, Quota: quota}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func userQuota(t *testing.T, r *Repo, userID int64) int64 {
	t.Helper()
	var u userRow
	if err := r.db.Take(&u, "id = ?", userID).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	return u.Quota
}

func mkCodes(tenantID int64, amountUSD float64, codes ...string) []*wallet.RedemptionCode {
	out := make([]*wallet.RedemptionCode, len(codes))
	for i, c := range codes {
		out[i] = &wallet.RedemptionCode{TenantID: tenantID, Code: c, AmountUSD: amountUSD}
	}
	return out
}

// TestCreateCodesWithDeduction_NoOverdraft 验证预扣不透支：不足整体拒绝、quota 不动、无码落库；
// 充足则原子扣减 + 建码。
func TestCreateCodesWithDeduction_NoOverdraft(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const owner = int64(10)
	seedUserQuota(t, r, owner, 1000) // 仅 1000 单位

	// 需 1500 > 1000：拒绝，且 quota 不变、无码。
	err := r.CreateCodesWithDeduction(ctx, 1, owner, 1500, mkCodes(1, 1, "a", "b", "c"))
	if err != wallet.ErrInsufficientQuota {
		t.Fatalf("overdraft = %v, want ErrInsufficientQuota", err)
	}
	if q := userQuota(t, r, owner); q != 1000 {
		t.Fatalf("quota moved on reject = %d, want 1000", q)
	}
	if list, _ := r.ListCodesByTenant(ctx, 1); len(list) != 0 {
		t.Fatalf("codes leaked on reject = %d, want 0", len(list))
	}

	// 需 1000 == 1000：成功，quota→0，3 张码入库。
	if err := r.CreateCodesWithDeduction(ctx, 1, owner, 1000, mkCodes(1, 1, "a", "b", "c")); err != nil {
		t.Fatalf("exact spend: %v", err)
	}
	if q := userQuota(t, r, owner); q != 0 {
		t.Fatalf("quota after spend = %d, want 0", q)
	}
	if list, _ := r.ListCodesByTenant(ctx, 1); len(list) != 3 {
		t.Fatalf("codes after spend = %d, want 3", len(list))
	}

	// 再扣：余额 0，拒绝。
	if err := r.CreateCodesWithDeduction(ctx, 1, owner, 1, mkCodes(1, 1, "z")); err != wallet.ErrInsufficientQuota {
		t.Fatalf("spend on empty = %v, want ErrInsufficientQuota", err)
	}
}

// TestCreateCodesWithDeduction_ConcurrentNoOverdraft 验证并发预扣条件 UPDATE 不击穿（quota 永不为负）。
func TestCreateCodesWithDeduction_ConcurrentNoOverdraft(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const owner = int64(10)
	seedUserQuota(t, r, owner, 1000) // 恰好够 10 次 ×100

	const n = 30 // 30 并发各扣 100，但只够 10 次
	var ok int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		idx := i
		go func() {
			defer wg.Done()
			code := "c" + string(rune('A'+idx))
			if err := r.CreateCodesWithDeduction(ctx, 1, owner, 100, mkCodes(1, 0.2, code)); err == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if ok != 10 {
		t.Fatalf("successful deductions = %d, want exactly 10", ok)
	}
	if q := userQuota(t, r, owner); q != 0 {
		t.Fatalf("final quota = %d, want 0 (no overdraft/no leak)", q)
	}
	if list, _ := r.ListCodesByTenant(ctx, 1); len(list) != 10 {
		t.Fatalf("codes created = %d, want 10 (1 per successful deduction)", len(list))
	}
}

// TestRedeemCode_SingleWinnerUnderConcurrency 验证并发兑换同一码：恰一人成功拿到面额，其余 USED。
func TestRedeemCode_SingleWinnerUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const owner = int64(10)
	seedUserQuota(t, r, owner, 100000)
	if err := r.CreateCodesWithDeduction(ctx, 1, owner, 50000, mkCodes(1, 5, "WIN")); err != nil {
		t.Fatalf("create code: %v", err)
	}

	const n = 40
	var winners int32
	var winAmount int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		uid := int64(100 + i)
		go func() {
			defer wg.Done()
			amt, err := r.RedeemCode(ctx, 1, "WIN", uid, time.Now())
			if err == nil {
				atomic.AddInt32(&winners, 1)
				if amt == 5 {
					atomic.AddInt32(&winAmount, 1)
				}
			} else if err != wallet.ErrRedeemCodeUsed {
				t.Errorf("loser err = %v, want ErrRedeemCodeUsed", err)
			}
		}()
	}
	wg.Wait()

	if winners != 1 || winAmount != 1 {
		t.Fatalf("winners=%d (amount==5: %d), want exactly 1", winners, winAmount)
	}
	// 已兑换后再兑换：USED（快速失败）。
	if _, err := r.RedeemCode(ctx, 1, "WIN", 999, time.Now()); err != wallet.ErrRedeemCodeUsed {
		t.Fatalf("re-redeem = %v, want ErrRedeemCodeUsed", err)
	}
}

// TestRedeemCode_CrossTenantDenied 验证跨租户兑换被拒（越权防线：tenant 不匹配即 invalid）。
func TestRedeemCode_CrossTenantDenied(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const owner = int64(10)
	seedUserQuota(t, r, owner, 100000)
	if err := r.CreateCodesWithDeduction(ctx, 1, owner, 50000, mkCodes(1, 5, "T1CODE")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 用租户 2 的上下文兑换租户 1 的码：查不到 -> INVALID（不泄漏存在性，不可越权）。
	if _, err := r.RedeemCode(ctx, 2, "T1CODE", 200, time.Now()); err != wallet.ErrRedeemCodeInvalid {
		t.Fatalf("cross-tenant redeem = %v, want ErrRedeemCodeInvalid", err)
	}
	// 码仍可被正确租户兑换（未被跨租户尝试消费）。
	if amt, err := r.RedeemCode(ctx, 1, "T1CODE", 201, time.Now()); err != nil || amt != 5 {
		t.Fatalf("legit redeem = (%v,%v), want (5,nil)", amt, err)
	}
	// 跨租户列表隔离。
	if l2, _ := r.ListCodesByTenant(ctx, 2); len(l2) != 0 {
		t.Fatalf("tenant2 sees %d codes, want 0", len(l2))
	}
}

// TestRedeemCode_InvalidAndDisabled 验证无效码/禁用码的错误码。
func TestRedeemCode_InvalidAndDisabled(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const owner = int64(10)
	seedUserQuota(t, r, owner, 100000)

	// 未知码 -> INVALID。
	if _, err := r.RedeemCode(ctx, 1, "ghost", 1, time.Now()); err != wallet.ErrRedeemCodeInvalid {
		t.Fatalf("unknown = %v, want ErrRedeemCodeInvalid", err)
	}

	// 已过期码 -> INVALID。
	expired := &wallet.RedemptionCode{TenantID: 1, Code: "OLD", AmountUSD: 5, ExpireAt: time.Now().Add(-time.Hour)}
	if err := r.EnsureRedemption(ctx, expired); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if _, err := r.RedeemCode(ctx, 1, "OLD", 1, time.Now()); err != wallet.ErrRedeemCodeInvalid {
		t.Fatalf("expired = %v, want ErrRedeemCodeInvalid", err)
	}
}

// TestRedeemCodeAndCredit_RejectsOverflowCredit 覆盖兑换入账的溢出兜底（安全审计纵深防御，利用链
// 最后一环）：即便某条旁路塞进一张面额大到 int64(amt×perUnit) 会溢出的码（正常建码已被面额上界 +
// decimal(20,8) 列宽挡死，此处用 EnsureRedemption 直接注入模拟），入账时必须拒（ErrRedeemCodeInvalid）
// 且分文不入账、事务回滚使码保持 enabled，绝不把无法忠实表示的额度搬进用户钱包。
func TestRedeemCodeAndCredit_RejectsOverflowCredit(t *testing.T) {
	ctx := context.Background()
	r := newRedemptionTestRepo(t)
	const redeemer = int64(200)
	const perUnit = 500000.0 // 生产口径 common.QuotaPerUnit
	seedUserQuota(t, r, redeemer, 0)

	// 面额 2e13 > 阈值 MaxInt64/perUnit ≈ 1.84e13 —— int64(amt×perUnit) 必溢出。
	bogus := &wallet.RedemptionCode{TenantID: 1, Code: "OVERFLOW", AmountUSD: 2e13}
	if err := r.EnsureRedemption(ctx, bogus); err != nil {
		t.Fatalf("seed bogus code: %v", err)
	}

	if _, _, err := r.RedeemCodeAndCredit(ctx, 1, "OVERFLOW", redeemer, time.Now(), perUnit); err != wallet.ErrRedeemCodeInvalid {
		t.Fatalf("overflow-credit redeem = %v, want ErrRedeemCodeInvalid", err)
	}
	// 分文不入账；事务回滚 → CAS 撤销，码保持 enabled（未作废，留待人工处置）。
	if q := userQuota(t, r, redeemer); q != 0 {
		t.Fatalf("redeemer credited on overflow = %d, want 0", q)
	}
	if list, _ := r.ListCodesByTenant(ctx, 1); len(list) != 1 || list[0].Status != wallet.RedemptionEnabled {
		t.Fatalf("code status after rejected redeem = %+v, want 1 code still Enabled", list)
	}
}

// TestRedeemCodeAndCredit_AtomicCreditAndRollback 锁定「翻 used 与入账额度同事务原子」：
//   - 正常：CAS 翻 used + 原生 quota 入账在同一事务内落地，redeemer.quota 精确增加、码翻 used；
//   - 回滚：入账 UPDATE 失败（此处删 users 表制造）→ 整个事务回滚，CAS 一并撤销，码保持 enabled
//     可再兑——杜绝旧「两步跨事务」下「码作废却不到账、无对账兜底」的悬空态。
func TestRedeemCodeAndCredit_AtomicCreditAndRollback(t *testing.T) {
	ctx := context.Background()
	const owner = int64(10)
	const redeemer = int64(200)
	const perUnit = 100.0 // $5 × 100 = 500 单位

	// —— 正常路径 ——
	r := newRedemptionTestRepo(t)
	seedUserQuota(t, r, owner, 100000)
	seedUserQuota(t, r, redeemer, 0)
	if err := r.CreateCodesWithDeduction(ctx, 1, owner, 50000, mkCodes(1, 5, "GIFT")); err != nil {
		t.Fatalf("create code: %v", err)
	}
	amt, credit, err := r.RedeemCodeAndCredit(ctx, 1, "GIFT", redeemer, time.Now(), perUnit)
	if err != nil {
		t.Fatalf("redeem+credit: %v", err)
	}
	if amt != 5 || credit != 500 {
		t.Fatalf("(amount,credit) = (%v,%d), want (5,500)", amt, credit)
	}
	if q := userQuota(t, r, redeemer); q != 500 {
		t.Fatalf("redeemer quota = %d, want 500 (credited in same tx)", q)
	}
	// 再兑换同一码：USED（已消费，幂等防重复入账）。
	if _, _, err := r.RedeemCodeAndCredit(ctx, 1, "GIFT", redeemer, time.Now(), perUnit); err != wallet.ErrRedeemCodeUsed {
		t.Fatalf("re-redeem = %v, want ErrRedeemCodeUsed", err)
	}
	if q := userQuota(t, r, redeemer); q != 500 {
		t.Fatalf("redeemer quota after re-redeem = %d, want 500 (no double credit)", q)
	}

	// —— 回滚路径：入账失败必须连带撤销 CAS ——
	r2 := newRedemptionTestRepo(t)
	seedUserQuota(t, r2, owner, 100000)
	if err := r2.CreateCodesWithDeduction(ctx, 1, owner, 50000, mkCodes(1, 5, "GIFT2")); err != nil {
		t.Fatalf("create code2: %v", err)
	}
	// 删掉 users 表 → 同事务内 UPDATE users 必报错，触发回滚。
	if err := r2.db.Migrator().DropTable(&userRow{}); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if _, _, err := r2.RedeemCodeAndCredit(ctx, 1, "GIFT2", redeemer, time.Now(), perUnit); err == nil {
		t.Fatalf("redeem with broken credit should error, got nil")
	}
	// 关键断言：CAS 随事务回滚，码仍 enabled（未被作废），无「作废且不到账」的悬空态。
	list, err := r2.ListCodesByTenant(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != wallet.RedemptionEnabled {
		t.Fatalf("code status after rollback = %+v, want 1 code still Enabled", list)
	}
}
