package risk

import (
	"context"
	"net"
	"sync"
	"time"

	"newapi-mt/internal/platform/appctx"
)

// systemClock 是默认的真实时钟。
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// ---- MemStatusChecker：内存封禁名单（按 userID / tenantID）----

// MemStatusChecker 是 StatusChecker 的内存假实现：命中封禁名单即非 active。
// 真实实现据 RiskRepo/Identity 判定；本轮供单测与早期装配。
type MemStatusChecker struct {
	mu            sync.RWMutex
	bannedUsers   map[int64]bool
	bannedTenants map[int64]bool
}

var _ StatusChecker = (*MemStatusChecker)(nil)

// NewMemStatusChecker 构造空名单（默认全部放行）。
func NewMemStatusChecker() *MemStatusChecker {
	return &MemStatusChecker{bannedUsers: map[int64]bool{}, bannedTenants: map[int64]bool{}}
}

// BanUser 封禁某用户。
func (m *MemStatusChecker) BanUser(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bannedUsers[userID] = true
}

// BanTenant 封禁某租户（其名下用户调用一律拒绝）。
func (m *MemStatusChecker) BanTenant(tenantID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bannedTenants[tenantID] = true
}

// Active 实现 StatusChecker。
func (m *MemStatusChecker) Active(_ context.Context, p *appctx.Principal) (bool, error) {
	if p == nil {
		return false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.bannedTenants[p.TenantID] || m.bannedUsers[p.UserID] {
		return false, nil
	}
	return true, nil
}

// ---- MemIPAllowlist：内存 IP allowlist（按 userID，支持精确 IP + CIDR）----

// MemIPAllowlist 是 IPAllowlist 的内存假实现。某用户未配置规则 → 放行（不限制）；
// 配置后客户端 IP 必须命中其一（精确或 CIDR）方可放行。真实实现据 RiskRepo 顺延。
type MemIPAllowlist struct {
	mu    sync.RWMutex
	rules map[int64][]ipRule // userID -> 规则集
}

type ipRule struct {
	ip  net.IP     // 精确匹配（net==nil 时使用）
	net *net.IPNet // CIDR 匹配
}

var _ IPAllowlist = (*MemIPAllowlist)(nil)

// NewMemIPAllowlist 构造空 allowlist。
func NewMemIPAllowlist() *MemIPAllowlist {
	return &MemIPAllowlist{rules: map[int64][]ipRule{}}
}

// Allow 为某用户追加一条允许规则，cidrOrIP 形如 "1.2.3.4" 或 "10.0.0.0/8"。
// 非法规则忽略并返回 false。
func (m *MemIPAllowlist) Allow(userID int64, cidrOrIP string) bool {
	r, ok := parseIPRule(cidrOrIP)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[userID] = append(m.rules[userID], r)
	return true
}

func parseIPRule(s string) (ipRule, bool) {
	if _, ipnet, err := net.ParseCIDR(s); err == nil {
		return ipRule{net: ipnet}, true
	}
	if ip := net.ParseIP(s); ip != nil {
		return ipRule{ip: ip}, true
	}
	return ipRule{}, false
}

// Allowed 实现 IPAllowlist。
func (m *MemIPAllowlist) Allowed(_ context.Context, p *appctx.Principal, ip string) (bool, error) {
	if p == nil {
		return false, nil
	}
	m.mu.RLock()
	rules := m.rules[p.UserID]
	m.mu.RUnlock()
	if len(rules) == 0 {
		return true, nil // 未配置 allowlist → 不限制
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false, nil // 配置了 allowlist 但来源 IP 不可解析 → 拒
	}
	for _, r := range rules {
		if r.net != nil && r.net.Contains(parsed) {
			return true, nil
		}
		if r.ip != nil && r.ip.Equal(parsed) {
			return true, nil
		}
	}
	return false, nil
}

// ---- MemRPMResolver：内存每主体 RPM（按 userID）----

// MemRPMResolver 是 RPMResolver 的内存假实现：按 userID 返回每窗口上限，未配置回退默认。
type MemRPMResolver struct {
	mu     sync.RWMutex
	byUser map[int64]int
}

var _ RPMResolver = (*MemRPMResolver)(nil)

// NewMemRPMResolver 构造空解析器。
func NewMemRPMResolver() *MemRPMResolver {
	return &MemRPMResolver{byUser: map[int64]int{}}
}

// Set 设定某用户的 RPM 上限。
func (m *MemRPMResolver) Set(userID int64, rpm int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byUser[userID] = rpm
}

// RPMFor 实现 RPMResolver。
func (m *MemRPMResolver) RPMFor(_ context.Context, p *appctx.Principal) (int, bool, error) {
	if p == nil {
		return 0, false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	rpm, ok := m.byUser[p.UserID]
	return rpm, ok, nil
}

// ---- MemAlertSink：内存告警收集 ----

// MemAlertSink 是 AlertSink 的内存假实现，记录已下发的告警供断言/早期装配。
type MemAlertSink struct {
	mu     sync.Mutex
	alerts []Alert
}

var _ AlertSink = (*MemAlertSink)(nil)

// NewMemAlertSink 构造空收集器。
func NewMemAlertSink() *MemAlertSink {
	return &MemAlertSink{}
}

// Fire 实现 AlertSink。
func (m *MemAlertSink) Fire(_ context.Context, a Alert) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, a)
}

// Alerts 返回已下发告警的副本。
func (m *MemAlertSink) Alerts() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Alert, len(m.alerts))
	copy(out, m.alerts)
	return out
}
