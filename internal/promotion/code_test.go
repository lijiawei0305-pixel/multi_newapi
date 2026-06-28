package promotion

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestValidatePrefix(t *testing.T) {
	cases := []struct {
		name string
		in   string // 已归一化输入
		ok   bool
	}{
		{"simple", "wechat", true},
		{"with-digits", "wx2024", true},
		{"with-hyphen", "abc-123", true},
		{"max-len", strings.Repeat("a", maxPrefixLen), true},
		{"empty", "", false},
		{"too-long", strings.Repeat("a", maxPrefixLen+1), false},
		{"underscore", "wechat_x", false},
		{"space", "we chat", false},
		{"dot", "we.chat", false},
		{"non-ascii", "wéchat", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePrefix(c.in)
			if c.ok && err != nil {
				t.Fatalf("validatePrefix(%q) = %v, want nil", c.in, err)
			}
			if !c.ok && !apperr.Is(err, "CHANNEL_PREFIX_INVALID") {
				t.Fatalf("validatePrefix(%q) = %v, want CHANNEL_PREFIX_INVALID", c.in, err)
			}
		})
	}
}

func TestNormalizePrefix(t *testing.T) {
	if got := normalizePrefix("  WeChat  "); got != "wechat" {
		t.Fatalf("normalizePrefix = %q, want wechat", got)
	}
}

func TestNewChannelCode(t *testing.T) {
	code, err := newChannelCode(strings.NewReader(strings.Repeat("x", 64)), "wechat")
	if err != nil {
		t.Fatalf("newChannelCode err = %v", err)
	}
	if !strings.HasPrefix(code, "wechat"+codeSep) {
		t.Fatalf("code = %q, want prefix wechat_", code)
	}
	// <prefix>_<2*randBytes hex>
	wantLen := len("wechat") + len(codeSep) + 2*randBytes
	if len(code) != wantLen {
		t.Fatalf("len(code) = %d, want %d", len(code), wantLen)
	}
	suffix := code[len("wechat")+len(codeSep):]
	for _, r := range suffix {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("suffix %q has non-hex char %q", suffix, r)
		}
	}
}

func TestNewChannelCode_EntropyError(t *testing.T) {
	sentinel := errors.New("no entropy")
	_, err := newChannelCode(failReader{err: sentinel}, "wechat")
	if !apperr.Is(err, "CHANNEL_CODE_GEN") {
		t.Fatalf("err = %v, want CHANNEL_CODE_GEN", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped sentinel", err)
	}
}

func TestSignupURLRoundTrip(t *testing.T) {
	code := "wechat_ab12cd34ef56ab78"
	u := signupURL(code)
	if !strings.HasPrefix(u, SignupPath+"?") {
		t.Fatalf("url = %q, want %s? prefix", u, SignupPath)
	}
	got, err := ParseChannelCode(u)
	if err != nil {
		t.Fatalf("ParseChannelCode(%q) err = %v", u, err)
	}
	if got != code {
		t.Fatalf("round-trip code = %q, want %q", got, code)
	}
}

func TestParseChannelCode(t *testing.T) {
	const code = "wechat_ab12"
	okCases := []struct {
		name string
		raw  string
	}{
		{"full-path", "/sign-up?channel=" + code},
		{"absolute-url", "https://aaa.wedreamhub.com/sign-up?channel=" + code},
		{"leading-question", "?channel=" + code},
		{"bare-query", "channel=" + code},
		{"extra-params", "/sign-up?channel=" + code + "&ref=poster"},
		{"surrounding-space", "  /sign-up?channel=" + code + "  "},
	}
	for _, c := range okCases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseChannelCode(c.raw)
			if err != nil {
				t.Fatalf("ParseChannelCode(%q) err = %v", c.raw, err)
			}
			if got != code {
				t.Fatalf("code = %q, want %q", got, code)
			}
		})
	}

	errCases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"no-channel-param", "/sign-up?ref=poster"},
		{"empty-value", "/sign-up?channel="},
		{"no-query", "/sign-up"},
		{"bare-code-only", code}, // 裸渠道码不是 channel=... 形态
		{"malformed-escape", "/sign-up?channel=%zz"},
	}
	for _, c := range errCases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseChannelCode(c.raw); !apperr.Is(err, "CHANNEL_NOT_FOUND") {
				t.Fatalf("ParseChannelCode(%q) err = %v, want CHANNEL_NOT_FOUND", c.raw, err)
			}
		})
	}
}
