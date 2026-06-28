package siteconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestAssetUpload(t *testing.T) {
	ctx := context.Background()

	t.Run("success binds tenant path, renames, records asset", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)

		url, err := svc.Upload(ctx, 42, File{Name: "My Photo.PNG", Data: pngData})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(url, "/tenants/42/assets/") {
			t.Errorf("url %q missing tenant path binding", url)
		}
		if strings.Contains(strings.ToLower(url), "photo") {
			t.Errorf("url %q leaked original filename", url)
		}
		if !strings.HasSuffix(url, ".png") {
			t.Errorf("url %q does not end with .png", url)
		}
		a, err := repo.GetAsset(ctx, 1)
		if err != nil {
			t.Fatalf("asset not recorded: %v", err)
		}
		if a.TenantID != 42 || a.Status != AssetStatusActive {
			t.Errorf("asset = %+v, want tenant 42 + active", a)
		}
		if a.ContentType != "image/png" {
			t.Errorf("ContentType = %q, want image/png", a.ContentType)
		}
		if _, ok := blob.Get(a.Key); !ok {
			t.Error("object not stored in blob")
		}
	})

	t.Run("jpeg normalized to .jpg", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		url, err := svc.Upload(ctx, 1, File{Name: "a.jpeg", Data: jpegData})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(url, ".jpg") {
			t.Errorf("url %q want .jpg suffix (jpeg normalized)", url)
		}
	})

	t.Run("two uploads get distinct random names", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		u1, _ := svc.Upload(ctx, 1, File{Name: "a.png", Data: pngData})
		u2, _ := svc.Upload(ctx, 1, File{Name: "a.png", Data: pngData})
		if u1 == u2 {
			t.Fatal("expected distinct random names across uploads")
		}
	})

	t.Run("rejects forbidden type without storing", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		_, err := svc.Upload(ctx, 1, File{Name: "x.svg", Data: []byte("<svg/>")})
		if !apperr.Is(err, "ASSET_TYPE_FORBIDDEN") {
			t.Fatalf("err = %v, want ASSET_TYPE_FORBIDDEN", err)
		}
		if _, e := repo.GetAsset(ctx, 1); !apperr.Is(e, "ASSET_NOT_FOUND") {
			t.Error("asset recorded despite rejection")
		}
	})

	t.Run("rejects too large", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		big := make([]byte, MaxAssetBytes+1)
		copy(big, []byte("\x89PNG\r\n\x1a\n"))
		_, err := svc.Upload(ctx, 1, File{Name: "x.png", Data: big})
		if !apperr.Is(err, "ASSET_TOO_LARGE") {
			t.Fatalf("err = %v, want ASSET_TOO_LARGE", err)
		}
	})

	t.Run("propagates blob error, no asset recorded", func(t *testing.T) {
		repo := NewMemRepo()
		blob := &fakeBlob{putErr: errors.New("s3 down")}
		svc := NewAssetService(repo, blob)
		_, err := svc.Upload(ctx, 1, File{Name: "a.png", Data: pngData})
		if !errors.Is(err, blob.putErr) {
			t.Fatalf("err = %v, want blob put error", err)
		}
		if _, e := repo.GetAsset(ctx, 1); !apperr.Is(e, "ASSET_NOT_FOUND") {
			t.Error("asset recorded despite blob failure")
		}
	})

	t.Run("propagates repo create error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.createErr = errors.New("db down")
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		if _, err := svc.Upload(ctx, 1, File{Name: "a.png", Data: pngData}); !errors.Is(err, repo.createErr) {
			t.Fatalf("err = %v, want repo create error", err)
		}
	})
}

func TestAssetTakedown(t *testing.T) {
	ctx := context.Background()

	t.Run("removes object and marks takendown (unreachable after)", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		if _, err := svc.Upload(ctx, 1, File{Name: "a.png", Data: pngData}); err != nil {
			t.Fatalf("upload: %v", err)
		}
		a, _ := repo.GetAsset(ctx, 1)
		if _, ok := blob.Get(a.Key); !ok {
			t.Fatal("precondition: object should exist before takedown")
		}
		if err := svc.Takedown(ctx, 1); err != nil {
			t.Fatalf("takedown: %v", err)
		}
		if _, ok := blob.Get(a.Key); ok {
			t.Error("object still accessible after takedown")
		}
		got, _ := repo.GetAsset(ctx, 1)
		if got.Status != AssetStatusTakenDown {
			t.Errorf("status = %q, want takendown", got.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := NewMemRepo()
		blob := NewMemBlob("https://cdn.test")
		svc := NewAssetService(repo, blob)
		if err := svc.Takedown(ctx, 999); !apperr.Is(err, "ASSET_NOT_FOUND") {
			t.Fatalf("err = %v, want ASSET_NOT_FOUND", err)
		}
	})

	t.Run("blob delete error keeps status active", func(t *testing.T) {
		repo := NewMemRepo()
		if err := repo.CreateAsset(ctx, &Asset{TenantID: 1, Key: "k"}); err != nil {
			t.Fatal(err)
		}
		blob := &fakeBlob{delErr: errors.New("s3 down")}
		svc := NewAssetService(repo, blob)
		if err := svc.Takedown(ctx, 1); !errors.Is(err, blob.delErr) {
			t.Fatalf("err = %v, want blob delete error", err)
		}
		got, _ := repo.GetAsset(ctx, 1)
		if got.Status == AssetStatusTakenDown {
			t.Error("status flipped despite blob failure")
		}
	})

	t.Run("propagates get-asset error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.getAssetErr = errors.New("db down")
		svc := NewAssetService(repo, NewMemBlob("https://cdn.test"))
		if err := svc.Takedown(ctx, 1); !errors.Is(err, repo.getAssetErr) {
			t.Fatalf("err = %v, want get-asset error", err)
		}
	})

	t.Run("propagates set-status error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.assets[1] = Asset{ID: 1, TenantID: 1, Key: "k", Status: AssetStatusActive}
		repo.setErr = errors.New("db down")
		svc := NewAssetService(repo, NewMemBlob("https://cdn.test"))
		if err := svc.Takedown(ctx, 1); !errors.Is(err, repo.setErr) {
			t.Fatalf("err = %v, want set-status error", err)
		}
	})
}

func TestNewAssetServiceNotNil(t *testing.T) {
	if NewAssetService(NewMemRepo(), NewMemBlob("")) == nil {
		t.Fatal("NewAssetService returned nil")
	}
}
