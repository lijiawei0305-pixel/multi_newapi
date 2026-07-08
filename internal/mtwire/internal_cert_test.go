package mtwire

// SSL 到期提醒(P3 #9)数据真实性回归:acme.sh --cron 自动续期只更新磁盘证书、不写 DB,
// cert-loop 需周期回刷——本端点供其列出全部 active 自定义域名(仅 active:pending/dns_verified
// 尚无已装证书,无从读到期时间)。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tenant"
)

// stubCDS 只覆写 ListAll;其余方法走内嵌 nil 接口(误调即 panic 暴露)。
type stubCDS struct {
	tenant.CustomDomainService
	list []tenant.CustomDomain
}

func (s stubCDS) ListAll(_ context.Context) ([]tenant.CustomDomain, error) {
	return s.list, nil
}

func TestHandleInternalListActiveCert_FiltersActiveOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{CustomDomains: stubCDS{list: []tenant.CustomDomain{
		{Domain: "a.example.com", Status: tenant.CustomDomainActive},
		{Domain: "b.example.com", Status: tenant.CustomDomainStatus("dns_verified")},
		{Domain: "c.example.com", Status: tenant.CustomDomainActive},
	}}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/internal/domain/active-cert", nil)

	app.HandleInternalListActiveCert(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Data struct {
			Domains []string `json:"domains"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Data.Domains) != 2 || env.Data.Domains[0] != "a.example.com" || env.Data.Domains[1] != "c.example.com" {
		t.Fatalf("domains = %v, want [a.example.com c.example.com](仅 active)", env.Data.Domains)
	}
}
