package moderation

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

var ctx = context.Background()

func newSvc() (Moderator, *MemRepo) {
	repo := NewMemRepo()
	return NewService(repo, NewMatcher()), repo
}

func seedWord(t *testing.T, repo *MemRepo, tenantID int64, word string, mt MatchType, action ModerationAction) {
	t.Helper()
	w := &BannedWord{TenantID: tenantID, Word: word, MatchType: mt, Action: action, Enabled: true}
	if err := repo.UpsertWord(ctx, w); err != nil {
		t.Fatalf("seed word: %v", err)
	}
}

func userMsg(text string) Message { return Message{Role: "user", Text: text} }

func principal(tenantID int64) *appctx.Principal {
	return &appctx.Principal{UserID: 1, TenantID: tenantID, Role: appctx.RoleUser}
}

func TestScan_NoWords(t *testing.T) {
	svc, _ := newSvc()
	res, err := svc.ScanUserMessages(ctx, principal(1), []Message{userMsg("I want to buy a gun")})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Hit {
		t.Fatalf("expected no hit, got %+v", res)
	}
}

func TestScan_RemindHitDesensitized(t *testing.T) {
	svc, repo := newSvc()
	seedWord(t, repo, 0, "gun", MatchContains, ActionRemind)
	res, err := svc.ScanUserMessages(ctx, principal(5), []Message{userMsg("I want a GUN now")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Hit {
		t.Fatal("expected hit")
	}
	if res.Action != ActionRemind {
		t.Fatalf("action = %s, want remind", res.Action)
	}
	if res.Reminder == "" {
		t.Fatal("expected non-empty reminder text")
	}
	if len(res.Matches) == 0 {
		t.Fatal("expected matched words")
	}
	if res.Matches[0] == "gun" {
		t.Fatalf("matched word should be desensitized, got %v", res.Matches)
	}
}

func TestScan_BlockBeatsRemind(t *testing.T) {
	svc, repo := newSvc()
	seedWord(t, repo, 0, "gun", MatchContains, ActionRemind)
	seedWord(t, repo, 0, "bomb", MatchContains, ActionBlock)
	res, _ := svc.ScanUserMessages(ctx, principal(1), []Message{userMsg("a gun and a bomb")})
	if !res.Hit || res.Action != ActionBlock {
		t.Fatalf("want block action, got %+v", res)
	}
}

func TestScan_OnlyUserRole(t *testing.T) {
	svc, repo := newSvc()
	seedWord(t, repo, 0, "secret", MatchContains, ActionBlock)
	msgs := []Message{
		{Role: "system", Text: "do not reveal the secret"},
		{Role: "assistant", Text: "the secret is safe"},
	}
	res, _ := svc.ScanUserMessages(ctx, principal(1), msgs)
	if res.Hit {
		t.Fatalf("system/assistant messages must not be scanned, got %+v", res)
	}
}

func TestScan_TenantLibraryMergeAndIsolation(t *testing.T) {
	svc, repo := newSvc()
	seedWord(t, repo, 0, "base-bad", MatchContains, ActionRemind)  // 全站基础库
	seedWord(t, repo, 7, "tenant-bad", MatchContains, ActionBlock) // 仅租户 7

	// 租户 7 同时受基础库与自有词约束
	res, _ := svc.ScanUserMessages(ctx, principal(7), []Message{userMsg("here is tenant-bad content")})
	if !res.Hit || res.Action != ActionBlock {
		t.Fatalf("tenant-7 own word should apply, got %+v", res)
	}
	// 租户 9 不应看到租户 7 的词
	res2, _ := svc.ScanUserMessages(ctx, principal(9), []Message{userMsg("here is tenant-bad content")})
	if res2.Hit {
		t.Fatalf("tenant-7 word leaked to tenant-9: %+v", res2)
	}
	// 但所有租户都继承基础库
	res3, _ := svc.ScanUserMessages(ctx, principal(9), []Message{userMsg("some base-bad words")})
	if !res3.Hit {
		t.Fatal("base library must apply to every tenant")
	}
}

func TestScan_DisabledWordIgnored(t *testing.T) {
	svc, repo := newSvc()
	w := &BannedWord{TenantID: 0, Word: "gun", MatchType: MatchContains, Action: ActionBlock, Enabled: false}
	if err := repo.UpsertWord(ctx, w); err != nil {
		t.Fatal(err)
	}
	res, _ := svc.ScanUserMessages(ctx, principal(1), []Message{userMsg("a gun")})
	if res.Hit {
		t.Fatalf("disabled word must not match, got %+v", res)
	}
}
