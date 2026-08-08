package common

import (
	"os"
	"strings"
)

// Main-site brand hosts that Turnstile protects when TurnstileCheckEnabled is on.
// Keep in sync with tenant.BaseDomain ("wedreamhub.com") — common must not import
// internal/tenant (dependency direction).
const (
	turnstileMainApex = "wedreamhub.com"
	turnstileMainWWW  = "www.wedreamhub.com"
)

// TURNSTILE_EXTRA_HOSTS: comma-separated FQDNs for platform-owned custom domains
// that should also run Turnstile (e.g. "portal.example.com,login.example.com").
// Agent OEM custom domains should NOT be listed here unless you also add them in
// the Cloudflare Turnstile hostname allowlist.
const EnvTurnstileExtraHosts = "TURNSTILE_EXTRA_HOSTS"

// NormalizeTurnstileHost lowercases Host, strips port and trailing dot.
func NormalizeTurnstileHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(h, ":"); i >= 0 {
		port := h[i+1:]
		if port != "" && isAllDigitsASCII(port) {
			h = h[:i]
		}
	}
	return strings.TrimSuffix(h, ".")
}

func isAllDigitsASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func turnstileExtraHostSet() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv(EnvTurnstileExtraHosts))
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		h := NormalizeTurnstileHost(part)
		if h != "" {
			out[h] = struct{}{}
		}
	}
	return out
}

// TurnstileAppliesToHost reports whether this request Host should show and
// enforce Cloudflare Turnstile when the global switch is enabled.
//
// Policy (方案 A):
//   - Main site only by default: apex + www.
//   - Agent subdomains (*.wedreamhub.com) and OEM custom domains: off unless
//     listed in TURNSTILE_EXTRA_HOSTS (and registered in CF Turnstile hostnames).
func TurnstileAppliesToHost(host string) bool {
	h := NormalizeTurnstileHost(host)
	if h == "" {
		return false
	}
	if h == turnstileMainApex || h == turnstileMainWWW {
		return true
	}
	if extra := turnstileExtraHostSet(); extra != nil {
		_, ok := extra[h]
		return ok
	}
	return false
}
