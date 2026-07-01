package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// ---- 纯函数：保留域名判定（表驱动）----

func TestIsReservedDomain(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		want   bool
	}{
		{"apex-itself", "wedreamhub.com", true},
		{"main-api", "api.wedreamhub.com", true},
		{"main-www", "www.wedreamhub.com", true},
		{"main-admin", "admin.wedreamhub.com", true},
		{"any-tenant-subdomain", "acme.wedreamhub.com", true},
		{"deep-subdomain", "a.b.wedreamhub.com", true},
		{"uppercase-normalized", "API.WeDreamHub.com", true},
		{"trailing-dot", "wedreamhub.com.", true},
		{"empty", "", true},
		// 非保留：代理自有域名
		{"agent-domain", "proxy.example.com", false},
		{"agent-apex", "example.com", false},
		{"agent-deep", "sub.proxy.example.com", false},
		{"lookalike-suffix", "notwedreamhub.com", false},
		{"evil-prefix", "wedreamhub.com.evil.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isReservedDomain(c.domain); got != c.want {
				t.Fatalf("isReservedDomain(%q) = %v, want %v", c.domain, got, c.want)
			}
		})
	}
}

// ---- 纯函数：FQDN 格式校验（表驱动）----

func TestValidDomainFormat(t *testing.T) {
	long64 := strings.Repeat("a", 64)
	cases := []struct {
		name   string
		domain string
		want   bool
	}{
		{"simple", "proxy.example.com", true},
		{"apex", "example.com", true},
		{"multi-label", "a.b.c.d.example.com", true},
		{"punycode", "xn--80ak6aa92e.com", true},
		{"hyphen-mid", "my-proxy.example.com", true},
		{"single-label", "localhost", false},
		{"empty", "", false},
		{"underscore", "proxy_.example.com", false},
		{"leading-hyphen", "-proxy.example.com", false},
		{"trailing-hyphen-label", "proxy-.example.com", false},
		{"empty-label", "proxy..example.com", false},
		{"ip-literal", "192.168.1.1", false},
		{"uppercase", "UPPER.example.com", false},
		{"label-too-long", long64 + ".example.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validDomainFormat(c.domain); got != c.want {
				t.Fatalf("validDomainFormat(%q) = %v, want %v", c.domain, got, c.want)
			}
		})
	}
}

// ---- 状态机迁移合法性（表驱动）----

func TestCustomDomainStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from CustomDomainStatus
		to   CustomDomainStatus
		want bool
	}{
		{CustomDomainPendingDNS, CustomDomainVerifying, true},
		{CustomDomainPendingDNS, CustomDomainFailed, true},
		{CustomDomainPendingDNS, CustomDomainActive, false},
		{CustomDomainPendingDNS, CustomDomainDNSVerified, false},
		{CustomDomainVerifying, CustomDomainDNSVerified, true},
		{CustomDomainVerifying, CustomDomainFailed, true},
		{CustomDomainVerifying, CustomDomainActive, false},
		{CustomDomainDNSVerified, CustomDomainActive, true},
		{CustomDomainDNSVerified, CustomDomainFailed, true},
		{CustomDomainDNSVerified, CustomDomainPendingDNS, false},
		{CustomDomainActive, CustomDomainFailed, true},
		{CustomDomainActive, CustomDomainActive, true}, // 续期幂等
		{CustomDomainActive, CustomDomainPendingDNS, false},
		{CustomDomainFailed, CustomDomainVerifying, true},
		{CustomDomainFailed, CustomDomainActive, false},
	}
	for _, c := range cases {
		t.Run(string(c.from)+"->"+string(c.to), func(t *testing.T) {
			if got := c.from.CanTransitionTo(c.to); got != c.want {
				t.Fatalf("%s.CanTransitionTo(%s) = %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

// ---- 内存假实现：CustomDomainRepo + DNSVerifier ----

type fakeCDRepo struct {
	byID   map[int64]*CustomDomain
	nextID int64
}

func newFakeCDRepo() *fakeCDRepo { return &fakeCDRepo{byID: map[int64]*CustomDomain{}} }

func (f *fakeCDRepo) CreateCustomDomain(_ context.Context, d *CustomDomain) error {
	for _, e := range f.byID {
		if e.Domain == d.Domain || e.TenantID == d.TenantID {
			return ErrDomainTaken
		}
	}
	f.nextID++
	d.ID = f.nextID
	cp := *d
	f.byID[d.ID] = &cp
	return nil
}

func (f *fakeCDRepo) GetCustomDomainByTenant(_ context.Context, tenantID int64) (*CustomDomain, error) {
	for _, e := range f.byID {
		if e.TenantID == tenantID {
			cp := *e
			return &cp, nil
		}
	}
	return nil, ErrCustomDomainNotFound
}

func (f *fakeCDRepo) GetCustomDomainByName(_ context.Context, domain string) (*CustomDomain, error) {
	for _, e := range f.byID {
		if e.Domain == domain {
			cp := *e
			return &cp, nil
		}
	}
	return nil, ErrCustomDomainNotFound
}

func (f *fakeCDRepo) UpdateCustomDomainStatus(_ context.Context, id int64, status CustomDomainStatus, lastError string) error {
	e, ok := f.byID[id]
	if !ok {
		return ErrCustomDomainNotFound
	}
	e.Status = status
	e.LastError = lastError
	return nil
}

func (f *fakeCDRepo) UpdateCustomDomainCert(_ context.Context, domain, certStatus string, expiresAt *time.Time, status CustomDomainStatus) error {
	for _, e := range f.byID {
		if e.Domain == domain {
			e.CertStatus = certStatus
			e.CertExpiresAt = expiresAt
			e.Status = status
			e.LastError = ""
			return nil
		}
	}
	return ErrCustomDomainNotFound
}

func (f *fakeCDRepo) DeleteCustomDomainByTenant(_ context.Context, tenantID int64) (string, error) {
	for id, e := range f.byID {
		if e.TenantID == tenantID {
			dom := e.Domain
			delete(f.byID, id)
			return dom, nil
		}
	}
	return "", ErrCustomDomainNotFound
}

func (f *fakeCDRepo) ListPendingCert(_ context.Context) ([]CustomDomain, error) {
	var out []CustomDomain
	for _, e := range f.byID {
		if e.Status == CustomDomainDNSVerified {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (f *fakeCDRepo) ListAllCustomDomains(_ context.Context) ([]CustomDomain, error) {
	out := make([]CustomDomain, 0, len(f.byID))
	for _, e := range f.byID {
		out = append(out, *e)
	}
	return out, nil
}

func (f *fakeCDRepo) DeleteCustomDomainByID(_ context.Context, id int64) (string, error) {
	e, ok := f.byID[id]
	if !ok {
		return "", ErrCustomDomainNotFound
	}
	dom := e.Domain
	delete(f.byID, id)
	return dom, nil
}

type fakeDNS struct {
	values map[string][]string
	err    error
}

func (f fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.values[name], nil
}

// ---- 服务：Bind ----

func TestCustomDomainService_Bind(t *testing.T) {
	ctx := context.Background()

	t.Run("reserved-rejected", func(t *testing.T) {
		svc := NewCustomDomainService(newFakeCDRepo(), fakeDNS{})
		_, err := svc.Bind(ctx, 1, "api.wedreamhub.com")
		if !apperr.Is(err, "DOMAIN_RESERVED") {
			t.Fatalf("want DOMAIN_RESERVED, got %v", err)
		}
	})

	t.Run("invalid-rejected", func(t *testing.T) {
		svc := NewCustomDomainService(newFakeCDRepo(), fakeDNS{})
		_, err := svc.Bind(ctx, 1, "localhost")
		if !apperr.Is(err, "DOMAIN_INVALID") {
			t.Fatalf("want DOMAIN_INVALID, got %v", err)
		}
	})

	t.Run("ok-pending", func(t *testing.T) {
		svc := NewCustomDomainService(newFakeCDRepo(), fakeDNS{})
		cd, err := svc.Bind(ctx, 1, "Proxy.Example.com") // 含大写：应被归一化
		if err != nil {
			t.Fatalf("Bind err = %v", err)
		}
		if cd.Domain != "proxy.example.com" {
			t.Fatalf("domain not normalized: %q", cd.Domain)
		}
		if cd.Status != CustomDomainPendingDNS {
			t.Fatalf("status = %q, want pending_dns", cd.Status)
		}
		if cd.VerifyToken == "" {
			t.Fatal("verify_token empty")
		}
	})

	t.Run("limit-second-domain", func(t *testing.T) {
		svc := NewCustomDomainService(newFakeCDRepo(), fakeDNS{})
		if _, err := svc.Bind(ctx, 1, "a.example.com"); err != nil {
			t.Fatalf("first bind err = %v", err)
		}
		_, err := svc.Bind(ctx, 1, "b.example.com")
		if !apperr.Is(err, "DOMAIN_LIMIT") {
			t.Fatalf("want DOMAIN_LIMIT, got %v", err)
		}
	})

	t.Run("taken-by-other-tenant", func(t *testing.T) {
		repo := newFakeCDRepo()
		svc := NewCustomDomainService(repo, fakeDNS{})
		if _, err := svc.Bind(ctx, 1, "shared.example.com"); err != nil {
			t.Fatalf("first bind err = %v", err)
		}
		_, err := svc.Bind(ctx, 2, "shared.example.com")
		if !apperr.Is(err, "DOMAIN_TAKEN") {
			t.Fatalf("want DOMAIN_TAKEN, got %v", err)
		}
	})
}

// ---- 服务：VerifyOwnership ----

func TestCustomDomainService_VerifyOwnership(t *testing.T) {
	ctx := context.Background()
	const domain = "proxy.example.com"

	bind := func(repo *fakeCDRepo, dns DNSVerifier) (CustomDomainService, *CustomDomain) {
		svc := NewCustomDomainService(repo, dns)
		cd, err := svc.Bind(ctx, 1, domain)
		if err != nil {
			t.Fatalf("setup bind err = %v", err)
		}
		return svc, cd
	}

	t.Run("match-goes-dns-verified", func(t *testing.T) {
		repo := newFakeCDRepo()
		// 先用空 DNS 绑定，拿到 token 后再设记录。
		svc := NewCustomDomainService(repo, nil)
		cd, err := svc.Bind(ctx, 1, domain)
		if err != nil {
			t.Fatalf("bind err = %v", err)
		}
		dns := fakeDNS{values: map[string][]string{
			VerifyTXTName(domain): {"other", cd.VerifyToken},
		}}
		svc = NewCustomDomainService(repo, dns)
		got, err := svc.VerifyOwnership(ctx, 1)
		if err != nil {
			t.Fatalf("verify err = %v", err)
		}
		if got.Status != CustomDomainDNSVerified {
			t.Fatalf("status = %q, want dns_verified", got.Status)
		}
		// 落库状态也应为 dns_verified（待发证信号）。
		stored, _ := repo.GetCustomDomainByTenant(ctx, 1)
		if stored.Status != CustomDomainDNSVerified {
			t.Fatalf("stored status = %q, want dns_verified", stored.Status)
		}
	})

	t.Run("no-match-fails", func(t *testing.T) {
		repo := newFakeCDRepo()
		dns := fakeDNS{values: map[string][]string{VerifyTXTName(domain): {"wrong-token"}}}
		svc, _ := bind(repo, dns)
		_, err := svc.VerifyOwnership(ctx, 1)
		if !apperr.Is(err, "DNS_VERIFY_FAILED") {
			t.Fatalf("want DNS_VERIFY_FAILED, got %v", err)
		}
		stored, _ := repo.GetCustomDomainByTenant(ctx, 1)
		if stored.Status != CustomDomainFailed || stored.LastError == "" {
			t.Fatalf("stored = %+v, want failed with last_error", stored)
		}
	})

	t.Run("dns-error-fails", func(t *testing.T) {
		repo := newFakeCDRepo()
		dns := fakeDNS{err: errors.New("nxdomain")}
		svc, _ := bind(repo, dns)
		_, err := svc.VerifyOwnership(ctx, 1)
		if !apperr.Is(err, "DNS_VERIFY_FAILED") {
			t.Fatalf("want DNS_VERIFY_FAILED, got %v", err)
		}
	})
}

// ---- 服务：MarkCertIssued（发证回写 → active）----

func TestCustomDomainService_MarkCertIssued(t *testing.T) {
	ctx := context.Background()
	const domain = "proxy.example.com"

	t.Run("dns-verified-goes-active", func(t *testing.T) {
		repo := newFakeCDRepo()
		svc := NewCustomDomainService(repo, nil)
		cd, _ := svc.Bind(ctx, 1, domain)
		// 直接把状态推到 dns_verified（模拟 TXT 已过）。
		_ = repo.UpdateCustomDomainStatus(ctx, cd.ID, CustomDomainDNSVerified, "")
		exp := time.Now().Add(90 * 24 * time.Hour)
		host, err := svc.MarkCertIssued(ctx, domain, "issued", &exp)
		if err != nil {
			t.Fatalf("MarkCertIssued err = %v", err)
		}
		if host != domain {
			t.Fatalf("host = %q, want %q", host, domain)
		}
		stored, _ := repo.GetCustomDomainByName(ctx, domain)
		if stored.Status != CustomDomainActive || stored.CertStatus != "issued" {
			t.Fatalf("stored = %+v, want active/issued", stored)
		}
	})

	t.Run("pending-cannot-go-active", func(t *testing.T) {
		repo := newFakeCDRepo()
		svc := NewCustomDomainService(repo, nil)
		if _, err := svc.Bind(ctx, 1, domain); err != nil {
			t.Fatalf("bind err = %v", err)
		}
		_, err := svc.MarkCertIssued(ctx, domain, "issued", nil)
		if !apperr.Is(err, "DOMAIN_STATUS_INVALID") {
			t.Fatalf("want DOMAIN_STATUS_INVALID, got %v", err)
		}
	})
}
