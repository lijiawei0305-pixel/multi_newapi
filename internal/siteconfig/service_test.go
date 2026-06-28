package siteconfig

import (
	"context"
	"errors"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestServiceGet(t *testing.T) {
	ctx := context.Background()

	t.Run("falls back to main-site default when unconfigured", func(t *testing.T) {
		svc := NewService(newFakeRepo())
		got, err := svc.Get(ctx, 7)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TenantID != 7 {
			t.Errorf("TenantID = %d, want 7 (backfilled)", got.TenantID)
		}
		if got.ThemeColor != defaultThemeColor {
			t.Errorf("ThemeColor = %q, want default %q", got.ThemeColor, defaultThemeColor)
		}
		if got.HomeMode != HomeModeDefault {
			t.Errorf("HomeMode = %q, want default", got.HomeMode)
		}
	})

	t.Run("returns stored config when configured", func(t *testing.T) {
		repo := newFakeRepo()
		repo.cfgs[3] = SiteConfig{TenantID: 3, SiteName: "Acme", ThemeColor: "#52c41a"}
		svc := NewService(repo)
		got, err := svc.Get(ctx, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.SiteName != "Acme" || got.ThemeColor != "#52c41a" {
			t.Errorf("got %+v, want stored Acme config", got)
		}
	})

	t.Run("propagates repo error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.getErr = errors.New("db down")
		svc := NewService(repo)
		if _, err := svc.Get(ctx, 1); !errors.Is(err, repo.getErr) {
			t.Fatalf("expected repo error to propagate, got %v", err)
		}
	})
}

func TestServicePatch(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects theme not in palette without touching storage", func(t *testing.T) {
		repo := newFakeRepo()
		svc := NewService(repo)
		err := svc.Patch(ctx, 1, SiteConfigPatch{ThemeColor: strptr("#abcdef")})
		if !apperr.Is(err, "THEME_NOT_IN_PALETTE") {
			t.Fatalf("err = %v, want THEME_NOT_IN_PALETTE", err)
		}
		if repo.upserted != nil {
			t.Fatal("storage written despite validation failure")
		}
	})

	t.Run("rejects custom_html home_mode", func(t *testing.T) {
		repo := newFakeRepo()
		svc := NewService(repo)
		err := svc.Patch(ctx, 1, SiteConfigPatch{HomeMode: hmptr(HomeModeCustomHTML)})
		if !apperr.Is(err, "HOME_MODE_LOCKED") {
			t.Fatalf("err = %v, want HOME_MODE_LOCKED", err)
		}
	})

	t.Run("creates config from default baseline when unconfigured", func(t *testing.T) {
		repo := newFakeRepo()
		svc := NewService(repo)
		if err := svc.Patch(ctx, 9, SiteConfigPatch{
			SiteName:   strptr("Acme"),
			ThemeColor: strptr("#52c41a"),
			HomeMode:   hmptr(HomeModeConfig),
		}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cur := repo.cfgs[9]
		if cur.TenantID != 9 {
			t.Errorf("TenantID = %d, want 9", cur.TenantID)
		}
		if cur.SiteName != "Acme" || cur.ThemeColor != "#52c41a" || cur.HomeMode != HomeModeConfig {
			t.Errorf("patched fields wrong: %+v", cur)
		}
		// 未触及字段取主站默认基线。
		if len(cur.EnabledModules) == 0 {
			t.Error("expected default EnabledModules from baseline")
		}
	})

	t.Run("merges onto existing config, untouched fields preserved", func(t *testing.T) {
		repo := newFakeRepo()
		repo.cfgs[2] = SiteConfig{TenantID: 2, SiteName: "Old", Footer: "keep", ThemeColor: "#1677ff"}
		svc := NewService(repo)
		if err := svc.Patch(ctx, 2, SiteConfigPatch{SiteName: strptr("New")}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cur := repo.cfgs[2]
		if cur.SiteName != "New" {
			t.Errorf("SiteName = %q, want New", cur.SiteName)
		}
		if cur.Footer != "keep" {
			t.Errorf("Footer = %q, want keep (untouched)", cur.Footer)
		}
	})

	t.Run("normalizes theme color to lowercase on apply", func(t *testing.T) {
		repo := newFakeRepo()
		svc := NewService(repo)
		if err := svc.Patch(ctx, 5, SiteConfigPatch{ThemeColor: strptr("#52C41A")}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := repo.cfgs[5].ThemeColor; got != "#52c41a" {
			t.Errorf("ThemeColor = %q, want normalized #52c41a", got)
		}
	})

	t.Run("applies all settable fields", func(t *testing.T) {
		repo := newFakeRepo()
		svc := NewService(repo)
		in := SiteConfigPatch{
			SiteName:        strptr("S"),
			LogoURL:         strptr("logo"),
			FaviconURL:      strptr("fav"),
			HeroTitle:       strptr("ht"),
			HeroSubtitle:    strptr("hs"),
			Announcement:    strptr("ann"),
			CustomerService: strptr("cs"),
			Footer:          strptr("ft"),
			ThemeColor:      strptr("#722ed1"),
			TemplateKey:     strptr("B"),
			HeroImageURL:    strptr("hero.png"),
			BannerJSON:      strptr("[]"),
			HomeMode:        hmptr(HomeModeConfig),
			CustomHTML:      strptr(""), // 空允许
			EnabledModules:  []string{"only"},
		}
		if err := svc.Patch(ctx, 1, in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := repo.cfgs[1]
		if c.SiteName != "S" || c.LogoURL != "logo" || c.FaviconURL != "fav" ||
			c.HeroTitle != "ht" || c.HeroSubtitle != "hs" || c.Announcement != "ann" ||
			c.CustomerService != "cs" || c.Footer != "ft" || c.ThemeColor != "#722ed1" ||
			c.TemplateKey != "B" || c.HeroImageURL != "hero.png" || c.BannerJSON != "[]" ||
			c.HomeMode != HomeModeConfig {
			t.Errorf("fields not all applied: %+v", c)
		}
		if len(c.EnabledModules) != 1 || c.EnabledModules[0] != "only" {
			t.Errorf("EnabledModules = %v, want [only]", c.EnabledModules)
		}
	})

	t.Run("propagates get error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.getErr = errors.New("db down")
		svc := NewService(repo)
		if err := svc.Patch(ctx, 1, SiteConfigPatch{SiteName: strptr("x")}); !errors.Is(err, repo.getErr) {
			t.Fatalf("expected get error, got %v", err)
		}
	})

	t.Run("propagates upsert error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.upsertErr = errors.New("db down")
		svc := NewService(repo)
		if err := svc.Patch(ctx, 1, SiteConfigPatch{SiteName: strptr("x")}); !errors.Is(err, repo.upsertErr) {
			t.Fatalf("expected upsert error, got %v", err)
		}
	})
}

func TestNewServiceNotNil(t *testing.T) {
	if NewService(newFakeRepo()) == nil {
		t.Fatal("NewService returned nil")
	}
}
