package gormrepo

// 覆盖 AppendEarningsBatch（异步计费 writer 定时 flush 的落库口）：与逐条 AppendEarning 语义等价——
// 幂等去重（同 idem_key 不双计）、按租户合并金额、每租户一次钱包 UPSERT、跨租户隔离、空批安全。
// 这是「触钱」路径，等价性是异步化不改账的核心保证。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
)

// TestAppendEarningsBatch_CoalescesIdempotentMultiTenant：一批里含同租户多条 + 一条幂等重复 + 另一租户，
// 断言 applied 只计首次入账、每租户钱包按「首次入账」金额合并累加、重放整批全幂等。
func TestAppendEarningsBatch_CoalescesIdempotentMultiTenant(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	batch := []agent.EarningEntry{
		{TenantID: 1, UserID: 100, SourceType: agent.SourceConsumeCommission, SourceID: "req-1", Amount: 10},
		{TenantID: 1, UserID: 100, SourceType: agent.SourceRatioMarkup, SourceID: "req-2", Amount: 5},
		{TenantID: 1, UserID: 100, SourceType: agent.SourceConsumeCommission, SourceID: "req-1", Amount: 10}, // 幂等重复
		{TenantID: 2, UserID: 200, SourceType: agent.SourceConsumeCommission, SourceID: "req-3", Amount: 7},
	}
	applied, err := r.AppendEarningsBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if applied != 3 {
		t.Fatalf("applied = %d, want 3（req-1 重复不计）", applied)
	}

	w1, _ := r.GetWallet(ctx, 1)
	if w1.WithdrawableBalance != 15 || w1.TotalEarned != 15 {
		t.Fatalf("tenant1 wallet = (%v, %v), want 15/15（10+5，重复只入一次）", w1.WithdrawableBalance, w1.TotalEarned)
	}
	w2, _ := r.GetWallet(ctx, 2)
	if w2.WithdrawableBalance != 7 || w2.TotalEarned != 7 {
		t.Fatalf("tenant2 wallet = (%v, %v), want 7/7（跨租户隔离）", w2.WithdrawableBalance, w2.TotalEarned)
	}

	// 整批重放：全部命中幂等键，applied=0，钱包纹丝不动。
	applied, err = r.AppendEarningsBatch(ctx, batch)
	if err != nil || applied != 0 {
		t.Fatalf("replay batch: applied=%d err=%v, want 0/nil", applied, err)
	}
	w1b, _ := r.GetWallet(ctx, 1)
	if w1b.WithdrawableBalance != 15 {
		t.Fatalf("tenant1 wallet after replay = %v, want 15（幂等不双计）", w1b.WithdrawableBalance)
	}
}

// TestAppendEarningsBatch_EquivalentToSequential：同一组条目，逐条 AppendEarning 与一次 AppendEarningsBatch
// 落到两个独立仓储后，每租户钱包（可提现/累计/owner）必须逐字段相等——批处理只合并写、不改账。
func TestAppendEarningsBatch_EquivalentToSequential(t *testing.T) {
	ctx := context.Background()
	seq := newTestRepo(t)
	bat := newTestRepo(t)

	entries := []agent.EarningEntry{
		{TenantID: 1, UserID: 100, SourceType: agent.SourceConsumeCommission, SourceID: "a", Amount: 3.5},
		{TenantID: 1, UserID: 100, SourceType: agent.SourceConsumeCommission, SourceID: "b", Amount: 1.25},
		{TenantID: 2, UserID: 200, SourceType: agent.SourceRatioMarkup, SourceID: "c", Amount: 9},
	}
	for _, e := range entries {
		if _, err := seq.AppendEarning(ctx, e); err != nil {
			t.Fatalf("seq append: %v", err)
		}
	}
	if _, err := bat.AppendEarningsBatch(ctx, entries); err != nil {
		t.Fatalf("batch: %v", err)
	}
	for _, tid := range []int64{1, 2} {
		ws, _ := seq.GetWallet(ctx, tid)
		wb, _ := bat.GetWallet(ctx, tid)
		if ws.WithdrawableBalance != wb.WithdrawableBalance || ws.TotalEarned != wb.TotalEarned || ws.UserID != wb.UserID {
			t.Fatalf("tenant %d 不等：seq=(%v,%v,u%d) batch=(%v,%v,u%d)", tid,
				ws.WithdrawableBalance, ws.TotalEarned, ws.UserID, wb.WithdrawableBalance, wb.TotalEarned, wb.UserID)
		}
	}
}

// TestAppendEarningsBatch_Empty：空批安全返回 (0, nil)，不开事务、不报错。
func TestAppendEarningsBatch_Empty(t *testing.T) {
	applied, err := newTestRepo(t).AppendEarningsBatch(context.Background(), nil)
	if applied != 0 || err != nil {
		t.Fatalf("empty batch: applied=%d err=%v, want 0/nil", applied, err)
	}
}
