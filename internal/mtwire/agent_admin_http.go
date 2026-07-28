package mtwire

// 主站管理 HTTP handler（AdminAuth；非租户维度）：设代理/列表/改代理/删代理/指标 + 提现审核。

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

type agentOut struct {
	ID               int64   `json:"id"` // = tenant_id
	OwnerUserID      int64   `json:"owner_user_id"`
	OwnerUsername    string  `json:"owner_username"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Level            int     `json:"level"`
	CanAPI           bool    `json:"can_api"`
	CostPriceCNY     float64 `json:"cost_price_cny"`
	PackageDiscount  float64 `json:"package_discount"`
	CommissionRatio  float64 `json:"commission_ratio"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
	DiscountRatio    float64 `json:"discount_ratio"`
	Status           string  `json:"status"`
	Subdomain        string  `json:"subdomain"`
	WithdrawableCNY  float64 `json:"withdrawable_cny"`
	FrozenCNY        float64 `json:"frozen_cny"`
	TotalEarnedCNY   float64 `json:"total_earned_cny"`
}

// agentMetricsOut 是 GET /api/admin/agents/:id/metrics 响应：代理升档决策的只读指标
// （总充值 / 累计分润 / 下级用户数），复用 reportrepo 财务聚合 + 一条下级计数薄查询。
type agentMetricsOut struct {
	RechargeTotalCNY    float64 `json:"recharge_total_cny"`
	CommissionEarnedCNY float64 `json:"commission_earned_cny"`
	DownstreamUserCount int64   `json:"downstream_user_count"`
}

// agentCreateIn 是 POST /api/admin/agents 入参。
type agentCreateIn struct {
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	OwnerUserID      int64   `json:"owner_user_id"`
	Level            int     `json:"level"`
	CostPriceCNY     float64 `json:"cost_price_cny"`
	PackageDiscount  float64 `json:"package_discount"`
	CommissionRatio  float64 `json:"commission_ratio"`
	DiscountFloor    float64 `json:"discount_floor"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
	DiscountRatio    float64 `json:"discount_ratio"`
}

// agentPatchIn 是 PATCH /api/admin/agents/:id 入参（指针支持局部更新）。
type agentPatchIn struct {
	Name             *string  `json:"name"`
	Level            *int     `json:"level"`
	CostPriceCNY     *float64 `json:"cost_price_cny"`
	PackageDiscount  *float64 `json:"package_discount"`
	CommissionRatio  *float64 `json:"commission_ratio"`
	DiscountFloor    *float64 `json:"discount_floor"`
	BottomPriceRatio *float64 `json:"bottom_price_ratio"`
	DiscountRatio    *float64 `json:"discount_ratio"`
	Status           *string  `json:"status"`
}

// ============================================================================
// 主站管理：设代理 / 列表 / 改代理（AdminAuth；非租户维度）
// ============================================================================

// HandleAdminCreateAgent POST /api/admin/agents —— 设代理。需 AdminAuth。
//
// 流程：① 校验参数/折扣；② 锁定 owner 用户并在锁内复查 1:1 占用；③ 建租户/域名；
// ④ 写 owner_user_id、重置 owner 归属、agent_profile 与钱包。生产 GORM 仓储下②—④在同一事务中，
// 任一步失败都不会留下孤立租户或半初始化代理。
func (a *App) HandleAdminCreateAgent(c *gin.Context) {
	var in agentCreateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	params := agent.AgentParams{
		UserID:           in.OwnerUserID,
		CostPrice:        in.CostPriceCNY,
		PackageDiscount:  in.PackageDiscount,
		CommissionRatio:  in.CommissionRatio,
		Level:            in.Level,
		DiscountFloor:    in.DiscountFloor,
		BottomPriceRatio: in.BottomPriceRatio,
		DiscountRatio:    in.DiscountRatio,
	}
	// ① 前置强校验（纯函数，不写库）：参数 + 折扣保护线。
	if err := params.Validate(); err != nil {
		respondErr(c, err)
		return
	}
	if err := pricing.NewGuard().ValidateGroupRatio(params.PackageDiscount, params.DiscountFloor); err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	if in.OwnerUserID <= 0 {
		respondErr(c, errAgentUserNotFound)
		return
	}
	var created *tenant.Tenant
	err := a.runAgentAdminMutation(ctx, a.TenantRepo != nil && a.AgentRepo != nil, func(m *agentAdminMutation) error {
		var owner struct {
			ID int64 `gorm:"column:id"`
		}
		ownerQuery := m.db.WithContext(ctx).Table("users").Select("id").Where("id = ?", in.OwnerUserID)
		if m.transactional {
			// MySQL/PostgreSQL 上串行化同一 owner 的并发设代理；SQLite 由写事务串行化。
			ownerQuery = ownerQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := ownerQuery.Take(&owner).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errAgentUserNotFound
			}
			return err
		}
		var occupied struct {
			ID int64 `gorm:"column:id"`
		}
		occupiedErr := m.db.WithContext(ctx).Table("tenants").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("owner_user_id = ?", in.OwnerUserID).Limit(1).Take(&occupied).Error
		if occupiedErr == nil {
			return errAgentOwnerTaken
		}
		if !errors.Is(occupiedErr, gorm.ErrRecordNotFound) {
			return occupiedErr
		}

		// 建租户（复用 tenant.Create：slug 校验 + 按档位派生子域名）。
		t, err := m.tenantService.Create(ctx, tenant.CreateTenantInput{
			Slug:             in.Slug,
			Name:             in.Name,
			TokenplanEnabled: true,
			SkipSubdomain:    params.Level < 1, // L0 普通：不发子域名（spec §5.2.2）
		})
		if err != nil {
			return err
		}
		if err := m.tenantRepo.SetOwnerUserID(ctx, t.ID, in.OwnerUserID); err != nil {
			return err
		}
		// owner 自身落「主站基准」(tenant_id=0)：代理 owner 自用按进货价/平台基准，不落任何代理店。
		res := m.db.WithContext(ctx).Table("users").Where("id = ?", in.OwnerUserID).Update("tenant_id", 0)
		if res.Error != nil {
			return res.Error
		}
		// owner 行已在上方加锁并验证存在。不用 RowsAffected 判定存在性：MySQL 对
		// tenant_id 本来就是 0 的幂等 UPDATE 可返回 0，误判会让主站用户无法设代理。
		if err := m.agentService.SetAgentType(ctx, t.ID, params); err != nil {
			return err
		}
		if err := m.agentRepo.EnsureWallet(ctx, t.ID, in.OwnerUserID); err != nil {
			return err
		}
		created = t
		return nil
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.buildAgentOut(ctx, created.ID, in.OwnerUserID, created.Slug, created.Name, string(created.Status), params))
}

// HandleAdminListAgents GET /api/admin/agents —— 代理列表（含 owner 用户名 + 钱包）。需 AdminAuth。
func (a *App) HandleAdminListAgents(c *gin.Context) {
	ctx := reqCtx(c)
	profiles, err := a.AgentRepo.ListProfiles(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	ids := make([]int64, 0, len(profiles))
	for _, p := range profiles {
		ids = append(ids, p.UserID)
	}
	names := a.usernamesByIDs(ctx, ids)
	out := make([]agentOut, 0, len(profiles))
	for _, p := range profiles {
		t, terr := a.TenantService.Get(ctx, p.TenantID)
		slug, name, status := "", "", ""
		if terr == nil && t != nil {
			slug, name, status = t.Slug, t.Name, string(t.Status)
		}
		if status == string(tenant.StatusDeleted) {
			continue // 已删除(归档)代理默认不在列表显示
		}
		w, _ := a.AgentService.GetWallet(ctx, p.TenantID)
		out = append(out, agentOut{
			ID:               p.TenantID,
			OwnerUserID:      p.UserID,
			OwnerUsername:    names[p.UserID],
			Slug:             slug,
			Name:             name,
			Level:            p.Level,
			CanAPI:           p.CanAPI,
			CostPriceCNY:     p.CostPriceCNY,
			PackageDiscount:  p.PackageDiscount,
			CommissionRatio:  p.CommissionRatio,
			BottomPriceRatio: p.BottomPriceRatio,
			DiscountRatio:    p.DiscountRatio,
			Status:           status,
			Subdomain:        a.TenantRepo.GetPrimaryDomain(ctx, p.TenantID),
			WithdrawableCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
			FrozenCNY:        walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
			TotalEarnedCNY:   walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
		})
	}
	respondOK(c, out)
}

// HandleAdminSetAgentDomain PUT /api/admin/agents/:id/domain —— 管理员给代理设子域名（label）。需 AdminAuth。
// 入参 {label} → `<label>.wedreamhub.com`（校验格式/保留词 + 全局查重 + 替换该代理现有主子域名）→ 失效 Host 缓存。
// 通配 nginx 已反代到 app,故插一条 tenant_domains 即建站生效（doc/agent-subdomain-and-delete.md §一）。
func (a *App) HandleAdminSetAgentDomain(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	var domain string
	var removed []string
	err = a.runAgentAdminMutation(ctx, a.TenantRepo != nil, func(m *agentAdminMutation) error {
		if err := lockAgentAdminTenant(ctx, m, tenantID); err != nil {
			return err
		}
		t, err := m.tenantService.Get(ctx, tenantID)
		if err != nil {
			return err
		}
		if isPlatformTenant(t) {
			return errAgentNotFound
		}
		if m.agentRepo == nil {
			return errAgentNotFound
		}
		if _, found, err := m.agentRepo.GetAgentType(ctx, tenantID); err != nil {
			return err
		} else if !found {
			return errAgentNotFound
		}
		domain, removed, err = m.tenantService.AddSubdomain(ctx, tenantID, strings.ToLower(strings.TrimSpace(in.Label)))
		return err
	})
	if err != nil {
		respondErr(c, err) // SLUG_INVALID / SLUG_RESERVED / DOMAIN_TAKEN
		return
	}
	if a.tenantCache != nil {
		for _, h := range removed {
			a.tenantCache.Invalidate(ctx, h)
		}
		a.tenantCache.Invalidate(ctx, domain)
	}
	respondOK(c, gin.H{"subdomain": domain})
}

// HandleAdminDeleteAgent DELETE /api/admin/agents/:id —— 删除代理（归档软删,doc/agent-subdomain-and-delete.md §二）。需 AdminAuth。
// 结算闸门(未提现/冻结收益>0 拒删) → status=deleted → 回收子域名+解绑自定义域名+失效缓存 → 终端用户迁回主站(tenant_id=0)。
// 数据留存归档,不硬删。
func (a *App) HandleAdminDeleteAgent(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	var removed []string
	var customDomain string
	err = a.runAgentAdminMutation(ctx, a.TenantRepo != nil && a.AgentRepo != nil, func(m *agentAdminMutation) error {
		if err := lockAgentAdminTenant(ctx, m, tenantID); err != nil {
			return err
		}
		t, err := m.tenantService.Get(ctx, tenantID)
		if err != nil {
			return err
		}
		if isPlatformTenant(t) {
			return errAgentNotFound
		}
		if m.agentRepo == nil {
			return errAgentNotFound
		}
		if _, found, err := m.agentRepo.GetAgentType(ctx, tenantID); err != nil {
			return err
		} else if !found {
			return errAgentNotFound
		}

		// 结算闸门必须 fail-closed：读钱包失败不得当作零余额继续删除。
		var w *agent.AgentWallet
		unsettled := false
		if m.transactional {
			var row struct {
				TenantID            int64 `gorm:"column:tenant_id"`
				WithdrawableUnits   int64 `gorm:"column:withdrawable_balance_units"`
				FrozenWithdrawUnits int64 `gorm:"column:frozen_withdraw_amount_units"`
			}
			walletErr := m.db.WithContext(ctx).Table("agent_wallets").
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("tenant_id", "withdrawable_balance_units", "frozen_withdraw_amount_units").
				Where("tenant_id = ?", tenantID).Take(&row).Error
			if walletErr != nil && !errors.Is(walletErr, gorm.ErrRecordNotFound) {
				return walletErr
			}
			unsettled = row.WithdrawableUnits != 0 || row.FrozenWithdrawUnits != 0
		} else {
			var walletErr error
			w, walletErr = m.agentService.GetWallet(ctx, tenantID)
			if walletErr != nil {
				return walletErr
			}
			unsettled = w != nil && (w.WithdrawableBalance != 0 || w.FrozenWithdrawAmount != 0)
		}
		if unsettled {
			return errAgentUnsettled
		}
		if err := m.tenantService.SetStatus(ctx, tenantID, tenant.StatusDeleted); err != nil {
			return err
		}
		removed, err = m.tenantRepo.DeleteDomainsByTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		if m.customDomains != nil {
			customDomain, err = m.customDomains.Unbind(ctx, tenantID)
			if err != nil && !errors.Is(err, tenant.ErrCustomDomainNotFound) {
				return err
			}
			if errors.Is(err, tenant.ErrCustomDomainNotFound) {
				customDomain = ""
			}
		}
		return m.db.WithContext(ctx).Exec("UPDATE users SET tenant_id = 0 WHERE tenant_id = ?", tenantID).Error
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	// 只在事务成功后失效缓存，回滚时保留原有可解析状态。
	if a.tenantCache != nil {
		for _, h := range removed {
			a.tenantCache.Invalidate(ctx, h)
		}
		if customDomain != "" {
			a.tenantCache.Invalidate(ctx, customDomain)
		}
	}
	respondOK(c, gin.H{"id": tenantID, "status": string(tenant.StatusDeleted)})
}

// HandleAdminUpdateAgent PATCH /api/admin/agents/:id —— 改代理（id = tenant_id）。需 AdminAuth。
// 支持局部更新类型/等级/成本/折扣/分润（重过保护线校验）与租户名/状态。
func (a *App) HandleAdminUpdateAgent(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var in agentPatchIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	var updated *tenant.Tenant
	var updatedParams agent.AgentParams
	err = a.runAgentAdminMutation(
		ctx,
		a.TenantRepo != nil && a.AgentRepo != nil && a.PromotionRepo != nil,
		func(m *agentAdminMutation) error {
			if err := lockAgentAdminTenant(ctx, m, tenantID); err != nil {
				return err
			}
			t, err := m.tenantService.Get(ctx, tenantID)
			if err != nil {
				return err
			}
			if isPlatformTenant(t) {
				return errAgentNotFound
			}
			if m.agentRepo == nil {
				return errAgentNotFound
			}
			curParams, found, err := m.agentRepo.GetAgentType(ctx, tenantID)
			if err != nil {
				return err
			}
			if !found {
				return errAgentNotFound
			}
			if in.Level != nil {
				curParams.Level = *in.Level
			}
			if in.CostPriceCNY != nil {
				curParams.CostPrice = *in.CostPriceCNY
			}
			if in.PackageDiscount != nil {
				curParams.PackageDiscount = *in.PackageDiscount
			}
			if in.CommissionRatio != nil {
				curParams.CommissionRatio = *in.CommissionRatio
			}
			if in.DiscountFloor != nil {
				curParams.DiscountFloor = *in.DiscountFloor
			}
			if in.BottomPriceRatio != nil {
				curParams.BottomPriceRatio = *in.BottomPriceRatio
			}
			if in.DiscountRatio != nil {
				curParams.DiscountRatio = *in.DiscountRatio
			}
			// 先做无副作用校验，避免测试桩/非 GORM 适配下先改基建再发现参数非法。
			if err := curParams.Validate(); err != nil {
				return err
			}
			if err := pricing.NewGuard().ValidateGroupRatio(curParams.PackageDiscount, curParams.DiscountFloor); err != nil {
				return err
			}
			// 升档所需的子域名、渠道作废、档位落库以及同请求的名称/状态变更统一提交。
			if in.Level != nil && *in.Level >= 1 {
				if err := m.tenantService.EnsureSubdomain(ctx, tenantID, t.Slug); err != nil {
					return err
				}
				if err := m.promotion.VoidChannelsByTenant(ctx, tenantID); err != nil {
					return err
				}
			}
			if err := m.agentService.SetAgentType(ctx, tenantID, curParams); err != nil {
				return err
			}
			if in.Name != nil && *in.Name != "" && *in.Name != t.Name {
				if err := m.tenantRepo.UpdateName(ctx, tenantID, *in.Name); err != nil {
					return err
				}
				t.Name = *in.Name
			}
			if in.Status != nil {
				if err := m.tenantService.SetStatus(ctx, tenantID, tenant.TenantStatus(*in.Status)); err != nil {
					return err
				}
				t.Status = tenant.TenantStatus(*in.Status)
			}
			updated = t
			updatedParams = curParams
			return nil
		},
	)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.buildAgentOut(ctx, tenantID, updated.OwnerUserID, updated.Slug, updated.Name, string(updated.Status), updatedParams))
}

// HandleAdminAgentMetrics GET /api/admin/agents/:id/metrics —— 代理升档决策指标（需 AdminAuth；id=tenant_id）。
// 只读复用 reportrepo：总充值=RechargePaid([1,now] 求和)；分润收益=WalletTotals.TotalEarnedCNY（累计）；
// 下级用户数=CountTenantUsers（薄查询）。绝不接受客户端传除 :id 外的任何口径。
func (a *App) HandleAdminAgentMetrics(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 总充值：生命周期窗口 [1, now] 上复用 RechargePaid（单租户 map 至多一条，求和即总额）。
	rechargeMap, err := a.ReportRepo.RechargePaid(ctx, &tenantID, 1, time.Now().Unix())
	if err != nil {
		respondErr(c, err)
		return
	}
	var rechargeTotal float64
	for _, v := range rechargeMap {
		rechargeTotal += v
	}
	// 分润收益：钱包累计已赚（生命周期；缺行返回零值不报错）。
	wallet, err := a.ReportRepo.WalletTotals(ctx, &tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 下级用户数：薄计数查询（tenant_id=? AND deleted_at IS NULL）。
	userCount, err := a.ReportRepo.CountTenantUsers(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, agentMetricsOut{
		RechargeTotalCNY:    round2(rechargeTotal),
		CommissionEarnedCNY: round2(wallet.TotalEarnedCNY),
		DownstreamUserCount: userCount,
	})
}

// ============================================================================
// 主站管理：提现审核（AdminAuth）
// ============================================================================

// HandleAdminListWithdrawals GET /api/admin/withdrawals —— 全部提现单（可选 ?status= 过滤）。需 AdminAuth。
func (a *App) HandleAdminListWithdrawals(c *gin.Context) {
	ctx := reqCtx(c)
	page, pageSize := parseWithdrawalPaging(c)
	paged := withdrawalPagingRequested(c)
	if !paged {
		rows, err := a.AgentRepo.ListWithdrawals(ctx, c.Query("status"))
		if err != nil {
			respondErr(c, err)
			return
		}
		respondOK(c, a.withdrawalOuts(ctx, rows))
		return
	}
	rows, total, err := a.AgentRepo.ListWithdrawalsPage(ctx, c.Query("status"), page, pageSize)
	if err != nil {
		respondErr(c, err)
		return
	}
	items := a.withdrawalOuts(ctx, rows)
	respondOK(c, withdrawalPageOut{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// HandleAdminApproveWithdrawal POST /api/admin/withdrawals/:id/approve —— 审核通过；冻结保持不动，
// 等管理员完成线下打款后再由 mark-paid 真正出账。需 AdminAuth。
func (a *App) HandleAdminApproveWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, true)
}

// HandleAdminRejectWithdrawal POST /api/admin/withdrawals/:id/reject —— 拒绝（解冻退回）。需 AdminAuth。
//
// 驳回理由字段名标准化为 remark（提现闭环补强 #3：前端统一发 remark，与后端绑定字段对齐，
// 不再走会丢理由的 reason/remark 不匹配）；已持久化进 agent_withdrawals.remark，并经
// withdrawalOut.Remark 在代理自助列表 + 管理端列表中一并返回。
func (a *App) HandleAdminRejectWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, false)
}

func (a *App) reviewWithdrawal(c *gin.Context, approve bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		Remark string `json:"remark"`
	}
	if err := decodeOptionalJSONObject(c, &body, agentFinancialRequestBodyLimit); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if err := a.Withdrawals.Review(reqCtx(c), id, approve, body.Remark); err != nil {
		respondErr(c, err) // WITHDRAW_NOT_PENDING / WITHDRAW_NOT_FOUND
		return
	}
	respondOK(c, gin.H{"id": id})
}

// HandleAdminMarkPaidWithdrawal POST /api/admin/withdrawals/:id/mark-paid —— 标记已打款
// （提现闭环补强 #2：approved→paid，CAS(WHERE status='approved')；扣减冻结资金，真正出账；
// 记打款单号/凭证 + 打款时间）。payout_ref 必填。需 AdminAuth。
func (a *App) HandleAdminMarkPaidWithdrawal(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		PayoutRef string `json:"payout_ref"`
	}
	if err := decodeOptionalJSONObject(c, &body, agentFinancialRequestBodyLimit); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if err := a.Withdrawals.MarkPaid(reqCtx(c), id, body.PayoutRef); err != nil {
		respondErr(c, err) // PAYOUT_REF_REQUIRED / PAYOUT_REF_DUPLICATE / WITHDRAW_NOT_APPROVED / WITHDRAW_NOT_FOUND
		return
	}
	respondOK(c, gin.H{"id": id})
}

// buildAgentOut 组装单个代理对象（含 owner 用户名 + 钱包）。
func (a *App) buildAgentOut(ctx context.Context, tenantID, ownerUserID int64, slug, name, status string, p agent.AgentParams) agentOut {
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	return agentOut{
		ID:               tenantID,
		OwnerUserID:      ownerUserID,
		OwnerUsername:    a.usernamesByIDs(ctx, []int64{ownerUserID})[ownerUserID],
		Slug:             slug,
		Name:             name,
		Level:            p.Level,
		CanAPI:           p.CanAPI,
		CostPriceCNY:     p.CostPrice,
		PackageDiscount:  p.PackageDiscount,
		CommissionRatio:  p.CommissionRatio,
		BottomPriceRatio: p.BottomPriceRatio,
		DiscountRatio:    p.DiscountRatio,
		Status:           status,
		Subdomain:        a.TenantRepo.GetPrimaryDomain(ctx, tenantID),
		WithdrawableCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
		FrozenCNY:        walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
		TotalEarnedCNY:   walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
	}
}

// agentAdminMutation 是管理端代理聚合的单次写入边界。生产路径中所有仓储和领域
// 服务都重绑到同一 *gorm.DB 事务，确保 tenant/agent/promotion/custom-domain/users 同成同败。
// transactional=false 仅供注入内存仓储/失败桩的领域单测使用。
type agentAdminMutation struct {
	db            *gorm.DB
	tenantRepo    *tenantrepo.Repo
	tenantService tenant.TenantService
	agentRepo     *agentrepo.Repo
	agentService  agent.AgentService
	promotion     promotion.PromotionService
	customDomains tenant.CustomDomainService
	transactional bool
}

// runAgentAdminMutation 为设代理、换子域名、删除与升档提供统一事务边界。
func (a *App) runAgentAdminMutation(
	ctx context.Context,
	transactional bool,
	fn func(*agentAdminMutation) error,
) error {
	if !transactional {
		return fn(&agentAdminMutation{
			db:            a.DB,
			tenantRepo:    a.TenantRepo,
			tenantService: a.TenantService,
			agentRepo:     a.AgentRepo,
			agentService:  a.AgentService,
			promotion:     a.Promotion,
			customDomains: a.CustomDomains,
		})
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tr := tenantrepo.New(tx)
		ar := agentrepo.New(tx)
		pr := promotionrepo.New(tx)
		return fn(&agentAdminMutation{
			db:            tx,
			tenantRepo:    tr,
			tenantService: tenant.NewService(tr, tenant.NewSlugValidator()),
			agentRepo:     ar,
			agentService:  agent.NewService(ar, pricing.NewGuard()),
			promotion:     promotion.NewService(pr),
			customDomains: tenant.NewCustomDomainService(tr, nil),
			transactional: true,
		})
	})
}

// lockAgentAdminTenant 串行化同一租户的删除/升档/状态变更，避免两个管理请求
// 同时基于旧状态计算并互相覆盖。SQLite 由写事务本身串行，MySQL/PostgreSQL 使用 FOR UPDATE。
func lockAgentAdminTenant(ctx context.Context, m *agentAdminMutation, tenantID int64) error {
	if !m.transactional {
		return nil
	}
	var row struct {
		ID int64 `gorm:"column:id"`
	}
	err := m.db.WithContext(ctx).Table("tenants").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", tenantID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tenant.ErrTenantNotFound
	}
	return err
}
