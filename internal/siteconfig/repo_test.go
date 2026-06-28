package siteconfig

import (
	"context"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestMemRepoConfig(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	// 未配置：found=false。
	if _, found, err := r.GetConfig(ctx, 1); err != nil || found {
		t.Fatalf("GetConfig miss = (found %v, err %v), want (false, nil)", found, err)
	}

	// 首建：回填时间戳。
	cfg := &SiteConfig{TenantID: 1, SiteName: "Acme", EnabledModules: []string{"a", "b"}}
	if err := r.UpsertConfig(ctx, cfg); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	got, found, err := r.GetConfig(ctx, 1)
	if err != nil || !found {
		t.Fatalf("GetConfig hit = (found %v, err %v)", found, err)
	}
	if got.SiteName != "Acme" {
		t.Errorf("SiteName = %q", got.SiteName)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("timestamps not set on create")
	}
	createdAt := got.CreatedAt

	// 返回切片为副本：改写不影响内部。
	got.EnabledModules[0] = "mutated"
	if again, _, _ := r.GetConfig(ctx, 1); again.EnabledModules[0] == "mutated" {
		t.Error("GetConfig returned slice aliasing internal state")
	}

	// 更新：保留 CreatedAt。
	if err := r.UpsertConfig(ctx, &SiteConfig{TenantID: 1, SiteName: "Acme2"}); err != nil {
		t.Fatal(err)
	}
	upd, _, _ := r.GetConfig(ctx, 1)
	if upd.SiteName != "Acme2" {
		t.Errorf("SiteName = %q, want Acme2", upd.SiteName)
	}
	if !upd.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt changed on update: %v != %v", upd.CreatedAt, createdAt)
	}
}

func TestMemRepoAsset(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	// 未找到。
	if _, err := r.GetAsset(ctx, 1); !apperr.Is(err, "ASSET_NOT_FOUND") {
		t.Fatalf("GetAsset miss err = %v", err)
	}
	if err := r.SetAssetStatus(ctx, 1, AssetStatusTakenDown); !apperr.Is(err, "ASSET_NOT_FOUND") {
		t.Fatalf("SetAssetStatus miss err = %v", err)
	}

	// 创建：回填 ID + 默认 active + 时间戳。
	a := &Asset{TenantID: 7, Key: "k"}
	if err := r.CreateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 {
		t.Errorf("ID = %d, want 1", a.ID)
	}
	if a.Status != AssetStatusActive {
		t.Errorf("Status = %q, want active default", a.Status)
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}

	// 自增 ID。
	a2 := &Asset{TenantID: 7, Key: "k2"}
	if err := r.CreateAsset(ctx, a2); err != nil {
		t.Fatal(err)
	}
	if a2.ID != 2 {
		t.Errorf("ID = %d, want 2", a2.ID)
	}

	// 置状态。
	if err := r.SetAssetStatus(ctx, 1, AssetStatusTakenDown); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetAsset(ctx, 1); got.Status != AssetStatusTakenDown {
		t.Errorf("Status = %q, want takendown", got.Status)
	}
}

func TestMemBlob(t *testing.T) {
	ctx := context.Background()
	b := NewMemBlob("https://cdn.test")

	if _, ok := b.Get("missing"); ok {
		t.Error("Get on empty should be false")
	}
	url, err := b.Put(ctx, "tenants/1/assets/x.png", []byte("data"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://cdn.test/tenants/1/assets/x.png" {
		t.Errorf("url = %q", url)
	}
	if d, ok := b.Get("tenants/1/assets/x.png"); !ok || string(d) != "data" {
		t.Errorf("Get = (%q, %v), want (data, true)", d, ok)
	}
	if err := b.Delete(ctx, "tenants/1/assets/x.png"); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get("tenants/1/assets/x.png"); ok {
		t.Error("object should be gone after Delete")
	}
}
