// Package agentplan 是「代理合作套餐」域：用户一次性付费购买一档代理套餐 → 升级为对应档位代理，
// 带有效期，到期自动降级为普通用户。与 tokenplan（用户 token 用量套餐）彻底分开，互不影响。
//
// 与 tokenplan 的区别：本域不做月度计量/额度桶，购买成功后的「激活」是给用户所属租户写入 agent
// 档位（Level / CanAPI / DiscountRatio），并记录到期时间；到期由 master 定时任务降级。
package agentplan

import (
	"math"
	"time"
)

// PlanStatus 主站代理套餐上下架状态。
type PlanStatus string

const (
	// PlanEnabled 已上架（可展示/购买）。
	PlanEnabled PlanStatus = "enabled"
	// PlanDisabled 已下架（管理员保留定义但停售）。
	PlanDisabled PlanStatus = "disabled"
)

// Valid 报告状态字面值是否合法。
func (s PlanStatus) Valid() bool { return s == PlanEnabled || s == PlanDisabled }

// IsEnabled 报告是否为已上架。
func (s PlanStatus) IsEnabled() bool { return s == PlanEnabled }

// Plan 是一档代理合作套餐（管理员后台可配）。
type Plan struct {
	ID   int64
	Code string // 自然键（basic / oem / api），seed 幂等基准
	Name string // 展示名（普通代理 / OEM 代理 / API 代理）
	Desc string // 卡片描述文案

	Price         float64 // 一次性开通价（¥）
	AnchorPrice   float64 // 原价（营销划线锚点，仅展示，不参与计费）
	DiscountLabel string  // 折扣角标（如 "5折" / "-50%"）

	// 授予能力：购买成功激活时写入 agent 档位（internal/agent.SetAgentType）。
	GrantLevel         int     // 代理等级：0=普通代理 / ≥1=OEM 独立（自定义域名+装修）
	GrantCanAPI        bool    // 是否授予开放 API 能力（API 代理）
	GrantDiscountRatio float64 // 该档全线批发折扣系数（>0 生效；<=0=不设）

	ValidDays int // 有效期天数（到期降级为普通用户）

	IsRecommended bool
	Badge         string
	Sort          int
	Status        PlanStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PlanInput 是管理员创建/更新代理套餐的入参。
type PlanInput struct {
	Code               string
	Name               string
	Desc               string
	Price              float64
	AnchorPrice        float64
	DiscountLabel      string
	GrantLevel         int
	GrantCanAPI        bool
	GrantDiscountRatio float64
	ValidDays          int
	IsRecommended      bool
	Badge              string
	Sort               int
	Status             PlanStatus
}

// Validate 校验入参合法性；任一非法返回 ErrPlanInputInvalid（AGENT_PLAN_INPUT_INVALID）。
func (in PlanInput) Validate() error {
	switch {
	case in.Code == "" || in.Name == "":
		return ErrPlanInputInvalid
	case !validAmount(in.Price) || in.Price < 0:
		return ErrPlanInputInvalid
	case !validAmount(in.AnchorPrice) || in.AnchorPrice < 0:
		return ErrPlanInputInvalid
	case in.GrantLevel < 0:
		return ErrPlanInputInvalid
	case !validAmount(in.GrantDiscountRatio) || in.GrantDiscountRatio < 0:
		return ErrPlanInputInvalid
	case in.ValidDays <= 0:
		return ErrPlanInputInvalid
	case !in.Status.Valid():
		return ErrPlanInputInvalid
	}
	return nil
}

// toPlan 把入参映射为待入库的领域对象（ID/时间戳由仓储回填）。
func (in PlanInput) toPlan() *Plan {
	return &Plan{
		Code:               in.Code,
		Name:               in.Name,
		Desc:               in.Desc,
		Price:              in.Price,
		AnchorPrice:        in.AnchorPrice,
		DiscountLabel:      in.DiscountLabel,
		GrantLevel:         in.GrantLevel,
		GrantCanAPI:        in.GrantCanAPI,
		GrantDiscountRatio: in.GrantDiscountRatio,
		ValidDays:          in.ValidDays,
		IsRecommended:      in.IsRecommended,
		Badge:              in.Badge,
		Sort:               in.Sort,
		Status:             in.Status,
	}
}

// validAmount 拒绝 NaN / ±Inf（金额入口防御）。
func validAmount(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
