package gormrepo

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/moderation"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

var ctx = context.Background()

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

func TestWordCRUDAndTenantScope(t *testing.T) {
	r := newTestRepo(t)

	base := &moderation.BannedWord{TenantID: 0, Word: "gun", MatchType: moderation.MatchContains, Action: moderation.ActionRemind, Enabled: true}
	if err := r.UpsertWord(ctx, base); err != nil {
		t.Fatal(err)
	}
	if base.ID == 0 {
		t.Fatal("ID not backfilled on insert")
	}
	tw := &moderation.BannedWord{TenantID: 7, Word: "bomb", MatchType: moderation.MatchExact, Action: moderation.ActionBlock, Enabled: true}
	if err := r.UpsertWord(ctx, tw); err != nil {
		t.Fatal(err)
	}

	// ListWords scoped exactly to tenant
	if got, _ := r.ListWords(ctx, 0); len(got) != 1 || got[0].Word != "gun" {
		t.Fatalf("ListWords(0) = %+v", got)
	}
	if got, _ := r.ListWords(ctx, 7); len(got) != 1 || got[0].Word != "bomb" {
		t.Fatalf("ListWords(7) = %+v", got)
	}

	// Update persists (toggle enabled off)
	base.Enabled = false
	if err := r.UpsertWord(ctx, base); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListWords(ctx, 0); got[0].Enabled {
		t.Fatal("enabled update not persisted")
	}

	// Cross-tenant delete is a no-op error; correct tenant succeeds.
	if err := r.DeleteWord(ctx, 9, tw.ID); !apperr.Is(err, "MODERATION_WORD_NOT_FOUND") {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	if err := r.DeleteWord(ctx, 7, tw.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListWords(ctx, 7); len(got) != 0 {
		t.Fatalf("delete not persisted, got %+v", got)
	}
}

func TestUpsertRejectsInvalid(t *testing.T) {
	r := newTestRepo(t)
	bad := &moderation.BannedWord{TenantID: 0, Word: "[unclosed", MatchType: moderation.MatchRegex, Action: moderation.ActionRemind, Enabled: true}
	if err := r.UpsertWord(ctx, bad); !apperr.Is(err, "MODERATION_WORD_INVALID") {
		t.Fatalf("expected MODERATION_WORD_INVALID, got %v", err)
	}
}

func TestViolationsRecordFilterIsolate(t *testing.T) {
	r := newTestRepo(t)

	ev := &moderation.ViolationEvent{TenantID: 3, UserID: 100, TokenID: 5, Model: "gpt-5.5", MatchedWords: []string{"g**"}, Excerpt: "a g** here", ActionTaken: moderation.ActionRemind}
	if err := r.Record(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if ev.ID == 0 {
		t.Fatal("violation ID not backfilled")
	}
	_ = r.Record(ctx, &moderation.ViolationEvent{TenantID: 3, UserID: 200, Model: "x", MatchedWords: []string{"b**"}, ActionTaken: moderation.ActionBlock})
	_ = r.Record(ctx, &moderation.ViolationEvent{TenantID: 9, UserID: 100, ActionTaken: moderation.ActionBlock})

	// Tenant 3 sees its 2 events; tenant isolation holds.
	if all, _ := r.ListForAdmin(ctx, 3, moderation.ViolationFilter{}); len(all) != 2 {
		t.Fatalf("tenant 3 events = %d, want 2", len(all))
	}
	if t9, _ := r.ListForAdmin(ctx, 9, moderation.ViolationFilter{}); len(t9) != 1 {
		t.Fatalf("tenant 9 events = %d, want 1", len(t9))
	}

	// Filter by user + matched_words JSON round-trip.
	u100, _ := r.ListForAdmin(ctx, 3, moderation.ViolationFilter{UserID: 100})
	if len(u100) != 1 || u100[0].UserID != 100 {
		t.Fatalf("user filter = %+v", u100)
	}
	if len(u100[0].MatchedWords) != 1 || u100[0].MatchedWords[0] != "g**" {
		t.Fatalf("matched_words round-trip = %+v", u100[0].MatchedWords)
	}
}
