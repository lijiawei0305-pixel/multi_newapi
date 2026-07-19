package mtwire

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/agentplan"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticAgentPlanCatalog struct {
	plan *agentplan.Plan
}

func (*staticAgentPlanCatalog) Create(context.Context, agentplan.PlanInput) (*agentplan.Plan, error) {
	return nil, agentplan.ErrPlanInputInvalid
}

func (*staticAgentPlanCatalog) Update(context.Context, int64, agentplan.PlanInput) error {
	return agentplan.ErrPlanInputInvalid
}

func (c *staticAgentPlanCatalog) Get(context.Context, int64) (*agentplan.Plan, error) {
	return c.plan, nil
}

func (*staticAgentPlanCatalog) List(context.Context) ([]agentplan.Plan, error) {
	return nil, nil
}

func TestHandlePurchaseAgentPlanRejectsMalformedOptionalBodyBeforeSideEffects(t *testing.T) {
	app := &App{AgentCatalog: &staticAgentPlanCatalog{plan: &agentplan.Plan{
		ID: 1, Code: "agent", Name: "Agent", Price: 100, ValidDays: 365, Status: agentplan.PlanEnabled,
	}}}

	for name, body := range map[string]string{
		"truncated":         `{"provider":`,
		"wrong field type":  `{"provider":123}`,
		"null body":         `null`,
		"wrong top-level":   `[]`,
		"trailing garbage":  `{} trailing`,
		"second JSON value": `{} {}`,
		"oversized body":    strings.Repeat(" ", int(purchaseRequestBodyLimit)+1),
	} {
		t.Run(name, func(t *testing.T) {
			c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, http.MethodPost, body,
				gin.Params{{Key: "id", Value: strconv.FormatInt(app.AgentCatalog.(*staticAgentPlanCatalog).plan.ID, 10)}})

			app.HandlePurchaseAgentPlan(c)

			resp := decodeResp(t, rec)
			assert.False(t, resp.Success)
			assert.Equal(t, payment.CodeOrderInvalid, resp.Code)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
