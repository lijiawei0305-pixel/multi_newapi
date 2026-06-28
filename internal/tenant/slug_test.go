package tenant

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestSlugValidator_Validate(t *testing.T) {
	v := NewSlugValidator()
	cases := []struct {
		name string
		slug string
		want string // "" 表示通过
	}{
		// 合法
		{"simple", "acme", ""},
		{"with-digits", "tenant123", ""},
		{"with-hyphen", "my-shop", ""},
		{"min-len", "abc", ""},
		{"alnum-mix", "a1b2c3", ""},
		{"max-len-63", "a234567890123456789012345678901234567890123456789012345678901bc", ""},
		// 保留词（逐个）
		{"reserved-www", "www", "SLUG_RESERVED"},
		{"reserved-api", "api", "SLUG_RESERVED"},
		{"reserved-admin", "admin", "SLUG_RESERVED"},
		{"reserved-root", "root", "SLUG_RESERVED"},
		{"reserved-dashboard", "dashboard", "SLUG_RESERVED"},
		{"reserved-static", "static", "SLUG_RESERVED"},
		{"reserved-cdn", "cdn", "SLUG_RESERVED"},
		{"reserved-status", "status", "SLUG_RESERVED"},
		{"reserved-support", "support", "SLUG_RESERVED"},
		// 格式非法
		{"empty", "", "SLUG_INVALID"},
		{"too-short", "ab", "SLUG_INVALID"},
		{"too-long-64", "a234567890123456789012345678901234567890123456789012345678901bcd", "SLUG_INVALID"},
		{"uppercase", "Acme", "SLUG_INVALID"},
		{"leading-hyphen", "-acme", "SLUG_INVALID"},
		{"trailing-hyphen", "acme-", "SLUG_INVALID"},
		{"space", "ac me", "SLUG_INVALID"},
		{"underscore", "ac_me", "SLUG_INVALID"},
		{"dot", "ac.me", "SLUG_INVALID"},
		{"unicode", "café", "SLUG_INVALID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := v.Validate(c.slug)
			got := ""
			if err != nil {
				got = apperr.CodeOf(err)
			}
			if got != c.want {
				t.Fatalf("Validate(%q) code = %q, want %q", c.slug, got, c.want)
			}
		})
	}
}

// 保留词表中的每个词都必须被拒绝（防止词表与校验逻辑漂移）。
func TestReservedSlugs_AllRejected(t *testing.T) {
	v := NewSlugValidator()
	words := ReservedSlugs()
	if len(words) != 9 {
		t.Fatalf("ReservedSlugs len = %d, want 9", len(words))
	}
	for _, w := range words {
		if !apperr.Is(v.Validate(w), "SLUG_RESERVED") {
			t.Errorf("reserved word %q not rejected as SLUG_RESERVED", w)
		}
	}
}

// ReservedSlugs 返回副本，修改不得影响内部词表。
func TestReservedSlugs_ReturnsCopy(t *testing.T) {
	a := ReservedSlugs()
	a[0] = "mutated"
	b := ReservedSlugs()
	if b[0] == "mutated" {
		t.Fatal("ReservedSlugs returned slice aliases internal state")
	}
}
