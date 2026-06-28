package promotion

import (
	"context"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestMemRepo_CreateAndGetByCode(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	a := &Channel{TenantID: 1, Prefix: "wechat", ChannelCode: "wechat_aa"}
	b := &Channel{TenantID: 1, Prefix: "weibo", ChannelCode: "weibo_bb"}
	if err := r.CreateChannel(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateChannel(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || b.ID == a.ID {
		t.Fatalf("ids not assigned uniquely: a=%d b=%d", a.ID, b.ID)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Error("timestamps not set on create")
	}

	got, err := r.GetChannelByCode(ctx, "wechat_aa")
	if err != nil || got.ID != a.ID {
		t.Fatalf("GetChannelByCode = %v, %v", got, err)
	}
	if _, err := r.GetChannelByCode(ctx, "missing"); !apperr.Is(err, "CHANNEL_NOT_FOUND") {
		t.Fatalf("GetChannelByCode(missing) err = %v", err)
	}
}

func TestMemRepo_DuplicatePrefix(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	if err := r.CreateChannel(ctx, &Channel{Prefix: "wechat", ChannelCode: "wechat_1"}); err != nil {
		t.Fatal(err)
	}
	err := r.CreateChannel(ctx, &Channel{Prefix: "wechat", ChannelCode: "wechat_2"})
	if !apperr.Is(err, "CHANNEL_PREFIX_DUP") {
		t.Fatalf("dup prefix err = %v, want CHANNEL_PREFIX_DUP", err)
	}
}

func TestMemRepo_IncrRegisteredCount(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	c := &Channel{Prefix: "wechat", ChannelCode: "wechat_1"}
	_ = r.CreateChannel(ctx, c)

	for i := 0; i < 3; i++ {
		if err := r.IncrRegisteredCount(ctx, c.ID); err != nil {
			t.Fatalf("IncrRegisteredCount err = %v", err)
		}
	}
	got, _ := r.GetChannelByCode(ctx, "wechat_1")
	if got.RegisteredCount != 3 {
		t.Fatalf("RegisteredCount = %d, want 3", got.RegisteredCount)
	}

	if err := r.IncrRegisteredCount(ctx, 9999); !apperr.Is(err, "CHANNEL_NOT_FOUND") {
		t.Fatalf("IncrRegisteredCount(missing) err = %v", err)
	}
}

func TestMemRepo_Attribution(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	if _, ok := r.GetAttribution(ctx, 1); ok {
		t.Fatal("empty repo should have no attribution")
	}
	a := &Attribution{UserID: 1, TenantID: 7, ChannelID: 3, ChannelCode: "wechat_x"}
	if err := r.CreateAttribution(ctx, a); err != nil {
		t.Fatal(err)
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	got, ok := r.GetAttribution(ctx, 1)
	if !ok || got.TenantID != 7 || got.ChannelID != 3 {
		t.Fatalf("GetAttribution = %+v, %v", got, ok)
	}
}
