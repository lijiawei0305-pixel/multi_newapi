package mtwire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agent"
)

// TestRequireAgentLevel_GatesByLevel: L0 → 403 AGENT_LEVEL_LOCKED（中止）；L1 → 放行。
func TestRequireAgentLevel_GatesByLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := agent.NewMemRepo()
	ctx := context.Background()
	_ = repo.SetAgentType(ctx, 7, agent.AgentParams{Level: 0}) // 普通
	_ = repo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1}) // 独立
	app := &App{AgentService: agent.NewService(repo, nil)}

	// L0：中止 + 403。
	w0 := httptest.NewRecorder()
	c0, _ := gin.CreateTestContext(w0)
	c0.Request = httptest.NewRequest(http.MethodGet, "/", nil) // ensureAgentLevel 需 c.Request.Context()
	c0.Set(ginKeyAgentTenant, int64(7))
	app.RequireAgentLevel(1)(c0)
	if !c0.IsAborted() || w0.Code != http.StatusForbidden {
		t.Fatalf("L0: aborted=%v code=%d, want abort/403", c0.IsAborted(), w0.Code)
	}

	// L1：放行（未中止）。
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c1.Set(ginKeyAgentTenant, int64(9))
	app.RequireAgentLevel(1)(c1)
	if c1.IsAborted() {
		t.Fatalf("L1: unexpectedly aborted (code=%d)", w1.Code)
	}
}
