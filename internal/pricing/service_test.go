package pricing

import (
	"context"
	"errors"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestServiceGroupRatio(t *testing.T) {
	ctx := context.Background()

	t.Run("group-specific ratio takes precedence", func(t *testing.T) {
		repo := newFakeRepo()
		repo.setDefaultRatio(1, 1.0)
		repo.setGroupRatio(1, 42, 0.9)
		svc := NewService(repo)

		got, err := svc.GroupRatio(ctx, 1, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 0.9 {
			t.Fatalf("ratio = %v, want 0.9 (group-specific)", got)
		}
	})

	t.Run("falls back to default when no group-specific ratio", func(t *testing.T) {
		repo := newFakeRepo()
		repo.setDefaultRatio(1, 1.5)
		// 故意只为另一个分组设专属倍率，证明回退按 groupID 精确判定。
		repo.setGroupRatio(1, 7, 0.5)
		svc := NewService(repo)

		got, err := svc.GroupRatio(ctx, 1, 99)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 1.5 {
			t.Fatalf("ratio = %v, want 1.5 (default fallback)", got)
		}
	})

	t.Run("propagates group lookup error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.groupErr = errors.New("db down")
		svc := NewService(repo)

		if _, err := svc.GroupRatio(ctx, 1, 1); !errors.Is(err, repo.groupErr) {
			t.Fatalf("expected group error to propagate, got %v", err)
		}
	})

	t.Run("propagates default lookup error on fallback", func(t *testing.T) {
		repo := newFakeRepo()
		repo.defaultErr = errors.New("db down")
		svc := NewService(repo)

		if _, err := svc.GroupRatio(ctx, 1, 1); !errors.Is(err, repo.defaultErr) {
			t.Fatalf("expected default error to propagate, got %v", err)
		}
	})
}

func TestServiceModelPrice(t *testing.T) {
	ctx := context.Background()

	t.Run("returns price when at or above floor", func(t *testing.T) {
		repo := newFakeRepo()
		mp := ModelPrice{Model: "gpt-4o", InputPrice: 0.005, OutputPrice: 0.015, MinFloorPrice: 0.005}
		repo.setModelPrice(1, mp)
		svc := NewService(repo)

		got, err := svc.ModelPrice(ctx, 1, "gpt-4o")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != mp {
			t.Fatalf("price = %+v, want %+v", got, mp)
		}
	})

	t.Run("blocks when input price below floor", func(t *testing.T) {
		repo := newFakeRepo()
		repo.setModelPrice(1, ModelPrice{Model: "cheap", InputPrice: 0.001, OutputPrice: 0.02, MinFloorPrice: 0.005})
		svc := NewService(repo)

		_, err := svc.ModelPrice(ctx, 1, "cheap")
		if got := apperr.CodeOf(err); got != CodePriceBelowProtection {
			t.Fatalf("error code = %q, want %q", got, CodePriceBelowProtection)
		}
	})

	t.Run("blocks when output price below floor", func(t *testing.T) {
		repo := newFakeRepo()
		repo.setModelPrice(1, ModelPrice{Model: "cheap-out", InputPrice: 0.01, OutputPrice: 0.002, MinFloorPrice: 0.005})
		svc := NewService(repo)

		_, err := svc.ModelPrice(ctx, 1, "cheap-out")
		if got := apperr.CodeOf(err); got != CodePriceBelowProtection {
			t.Fatalf("error code = %q, want %q", got, CodePriceBelowProtection)
		}
	})

	t.Run("propagates repo error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.modelErr = errors.New("db down")
		svc := NewService(repo)

		if _, err := svc.ModelPrice(ctx, 1, "x"); !errors.Is(err, repo.modelErr) {
			t.Fatalf("expected repo error to propagate, got %v", err)
		}
	})
}

func TestNewServiceNotNil(t *testing.T) {
	if NewService(newFakeRepo()) == nil {
		t.Fatal("NewService returned nil")
	}
}
