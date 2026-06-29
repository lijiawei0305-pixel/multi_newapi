package mtwire

// 2D 倍率（层级 × 模型分组）—— 在 new-api 基座内的装配与 HTTP 层（Phase 1，见 doc/detailed-design.md §2.15）。
//
// 计费倍率从一维升级为二维相乘：最终 groupRatio = GroupRatio[UserGroup(层级)] ×
// ( UsingGroup ∈ model_groups ? GroupRatio[UsingGroup] : 1 )。倍率真源是原生 GroupRatio；
// model_groups 登记表只标记「哪些 group 是模型分组」。接线经 grouphook.ModelGroup2DResolver 旁路覆盖
// new-api 计费单点 relay/helper.HandleGroupRatio（预扣与结算共用）。安全第一：miss/错误/未装配一律回退。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/platform/grouphook"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// 模型分组管理端点错误码。
var (
	errModelGroupInputInvalid = apperr.New("MODEL_GROUP_INPUT_INVALID", "模型分组入参非法", http.StatusBadRequest)
	errModelGroupNotFound     = apperr.New("MODEL_GROUP_NOT_FOUND", "模型分组不存在", http.StatusNotFound)
	errModelGroupNameTaken    = apperr.New("MODEL_GROUP_NAME_TAKEN", "模型分组名已存在", http.StatusConflict)
)

// groupRatioOf 解析某层级 / 分组的原生 GroupRatio。包级 seam：默认 ratio_setting.GetGroupRatio，
// 单测可注入（验证相乘 / panic 兜底），与 subscription_bridge.go 的 activateNativeSubHook 同模式。
var groupRatioOf = ratio_setting.GetGroupRatio

// ============================================================================
// 钩子装配 + 解析实现（旁路覆盖 /v1 计费 groupRatio）
// ============================================================================

// InstallModelGroup2DHook 注入「2D 倍率」旁路钩子（grouphook.ModelGroup2DResolver）。
// 由 SetMtRouter 在所有节点调用一次；未调用（如单测）时钩子为 nil，计费侧全程走原生倍率。
func (a *App) InstallModelGroup2DHook() {
	grouphook.ModelGroup2DResolver = a.resolveModelGroup2D
	grouphook.ModelGroupDropdownResolver = a.resolveModelGroupDropdown
}

// resolveModelGroupDropdown 是 grouphook.ModelGroupDropdownResolver 实现：仅「已登记模型分组」返
// (2D 有效扣费倍率, true)，否则 (0, false)。建 Key 下拉据此**只显示模型分组**（排除层级/default），
// 倍率为含代理租户覆盖的实际扣费值（= resolveModelGroup2D 的 层级×模型分组覆盖）。panic→(0,false)。
func (a *App) resolveModelGroupDropdown(userID int64, userGroup, group string) (ratio float64, isModelGroup bool) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: resolveModelGroupDropdown panic recovered")
			ratio, isModelGroup = 0, false
		}
	}()
	if a.ModelGroupRepo == nil || group == "" || !a.ModelGroupRepo.IsModelGroup(group) {
		return 0, false
	}
	eff, ok := a.resolveModelGroup2D(userID, userGroup, group)
	if !ok {
		return 0, false
	}
	return eff, true
}

// resolveModelGroup2D 是 grouphook.ModelGroup2DResolver 实现（含代理 per-tenant 覆盖）：
//
//	tier        = GroupRatio[userGroup]                                   （层级倍率，原生真源）
//	modelFactor = IsModelGroup(usingGroup)
//	              ? ( 该用户所属租户对此模型分组有 enabled 覆盖 ? 覆盖值 : GroupRatio[usingGroup] )
//	              : 1                                                      （模型分组折扣系数，未登记=1）
//	return (tier * modelFactor, true)
//
// 代理租户的 markup 覆盖（tenant_groups[tenant, model_group]）只在 usingGroup 是模型分组时叠入 modelFactor：
// 命中覆盖用覆盖值（代理加价）、否则用平台基准；受写入端「组合下限」保护（覆盖值 ≥ 平台基准）。
// 边界天然安全：usingGroup 是层级名 / 非模型分组 → 系数 1，仅层级，不重复算；主站用户 / userID=0 → 无覆盖，走基准。
// 旁路安全：自带 panic 兜底；未装配（ModelGroupRepo=nil）/ 异常 → (0,false)，由计费侧回退原生倍率，绝不破坏计费。
func (a *App) resolveModelGroup2D(userID int64, userGroup, usingGroup string) (ratio float64, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: resolveModelGroup2D panic recovered")
			ratio, ok = 0, false
		}
	}()
	if a.ModelGroupRepo == nil {
		return 0, false // 未装配：回退原生倍率
	}
	tier := groupRatioOf(userGroup)
	factor := 1.0
	if usingGroup != "" && a.ModelGroupRepo.IsModelGroup(usingGroup) {
		factor = groupRatioOf(usingGroup) // 平台基准
		// 代理 per-tenant 覆盖（仅模型分组）：命中即用覆盖值（写入端已保证 ≥ 基准，代理加价）。
		if override, hit := a.resolveTenantGroupRatio(context.Background(), userID, usingGroup); hit {
			factor = override
		}
	}
	return tier * factor, true
}

// ============================================================================
// 后台「模型分组管理」API（/api/admin/model-groups，AdminAuth）
// ============================================================================

// modelGroupOut 是模型分组管理列表/详情 DTO。ratio 取自当前 GetGroupRatio（真源），非本表列。
type modelGroupOut struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Ratio       float64 `json:"ratio"`
	ChannelID   *int64  `json:"channel_id"`
	ChannelName string  `json:"channel_name,omitempty"`
	Description string  `json:"description"`
	Enabled     bool    `json:"enabled"`
	Sort        int     `json:"sort"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// modelGroupCreateIn 是 POST 入参。enabled 缺省 true。
type modelGroupCreateIn struct {
	Name        string  `json:"name"`
	Ratio       float64 `json:"ratio"`
	ChannelID   *int64  `json:"channel_id"`
	Description string  `json:"description"`
	Enabled     *bool   `json:"enabled"`
	Sort        int     `json:"sort"`
}

// modelGroupUpdateIn 是 PUT 局部更新入参（指针字段，仅传则改）。
type modelGroupUpdateIn struct {
	Ratio       *float64 `json:"ratio"`
	ChannelID   *int64   `json:"channel_id"`
	Description *string  `json:"description"`
	Enabled     *bool    `json:"enabled"`
	Sort        *int     `json:"sort"`
}

// HandleAdminListModelGroups GET /api/admin/model-groups —— 列出全部模型分组（含当前倍率 + 绑定渠道）。需 AdminAuth。
func (a *App) HandleAdminListModelGroups(c *gin.Context) {
	ctx := reqCtx(c)
	rows, err := a.ModelGroupRepo.List(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	chNames := a.channelNamesByIDs(ctx, modelGroupChannelIDs(rows))
	out := make([]modelGroupOut, 0, len(rows))
	for _, m := range rows {
		out = append(out, toModelGroupOut(m, channelNameOf(m.ChannelID, chNames)))
	}
	respondOK(c, out)
}

// HandleAdminCreateModelGroup POST /api/admin/model-groups —— 新建模型分组 + 同步真源。需 AdminAuth。
//
// 同步：① 登记 model_groups；② GroupRatio[name]=ratio（倍率真源）；③ UserUsableGroups += name
// （用户建 Key 下拉可选）。②③ 经 model.UpdateOptionsBulk 单事务原子持久化 + 传播。
func (a *App) HandleAdminCreateModelGroup(c *gin.Context) {
	var in modelGroupCreateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || in.Ratio < 0 {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	ctx := reqCtx(c)
	mg, err := a.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{
		Name:        name,
		ChannelID:   in.ChannelID,
		Description: in.Description,
		Enabled:     enabled,
		Sort:        in.Sort,
	})
	if err != nil {
		respondErr(c, translateModelGroupErr(err))
		return
	}
	if err := a.syncModelGroupRatioAndUsable(name, in.Ratio, in.Description); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toModelGroupOut(*mg, ""))
}

// HandleAdminUpdateModelGroup PUT /api/admin/model-groups/:name —— 改 ratio/描述/启用/渠道/排序 + 同步真源。需 AdminAuth。
func (a *App) HandleAdminUpdateModelGroup(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	var in modelGroupUpdateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	if in.Ratio != nil && *in.Ratio < 0 {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// ① 更新 model_groups 元数据（channel/description/enabled/sort）。
	if err := a.ModelGroupRepo.Update(ctx, name, modelgroup.ModelGroupUpdate{
		ChannelID:   in.ChannelID,
		Description: in.Description,
		Enabled:     in.Enabled,
		Sort:        in.Sort,
	}); err != nil {
		respondErr(c, translateModelGroupErr(err))
		return
	}
	// ② 若改了 ratio，同步回真源 GroupRatio[name]。
	if in.Ratio != nil {
		if err := a.updateGroupRatioOption(name, *in.Ratio); err != nil {
			respondErr(c, err)
			return
		}
	}
	respondOK(c, gin.H{"name": name})
}

// HandleAdminDeleteModelGroup DELETE /api/admin/model-groups/:name —— 删登记 + 从 UserUsableGroups 移除。需 AdminAuth。
// 保留 GroupRatio[name]（删后 IsModelGroup=false → 系数 1，不影响计费；倍率残留无害）。
func (a *App) HandleAdminDeleteModelGroup(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		respondErr(c, errModelGroupInputInvalid)
		return
	}
	ctx := reqCtx(c)
	if err := a.ModelGroupRepo.Delete(ctx, name); err != nil {
		respondErr(c, translateModelGroupErr(err))
		return
	}
	if err := a.removeUserUsableGroup(name); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"name": name})
}

// ============================================================================
// 真源同步：GroupRatio（倍率）+ UserUsableGroups（用户可选分组）
// ============================================================================

// syncModelGroupRatioAndUsable 建模型分组后同步：GroupRatio[name]=ratio + UserUsableGroups += name。
// 两项经 model.UpdateOptionsBulk 单事务原子持久化（DB）+ 传播（内存 OptionMap/typed settings）。
func (a *App) syncModelGroupRatioAndUsable(name string, ratio float64, desc string) error {
	gr := ratio_setting.GetGroupRatioCopy()
	gr[name] = ratio
	grJSON, err := json.Marshal(gr)
	if err != nil {
		return err
	}
	opts := map[string]string{"GroupRatio": string(grJSON)}

	uug := setting.GetUserUsableGroupsCopy()
	if _, exists := uug[name]; !exists {
		label := strings.TrimSpace(desc)
		if label == "" {
			label = name
		}
		uug[name] = label
		uugJSON, err := json.Marshal(uug)
		if err != nil {
			return err
		}
		opts["UserUsableGroups"] = string(uugJSON)
	}
	return model.UpdateOptionsBulk(opts)
}

// updateGroupRatioOption 把 GroupRatio[name]=ratio 写回真源（持久化 + 传播）。
func (a *App) updateGroupRatioOption(name string, ratio float64) error {
	gr := ratio_setting.GetGroupRatioCopy()
	gr[name] = ratio
	grJSON, err := json.Marshal(gr)
	if err != nil {
		return err
	}
	return model.UpdateOption("GroupRatio", string(grJSON))
}

// removeUserUsableGroup 从 UserUsableGroups 移除 name（删模型分组后用户下拉不再出现）。无该项则空操作。
func (a *App) removeUserUsableGroup(name string) error {
	uug := setting.GetUserUsableGroupsCopy()
	if _, exists := uug[name]; !exists {
		return nil
	}
	delete(uug, name)
	uugJSON, err := json.Marshal(uug)
	if err != nil {
		return err
	}
	return model.UpdateOption("UserUsableGroups", string(uugJSON))
}

// ============================================================================
// 辅助
// ============================================================================

// toModelGroupOut 映射 DTO；ratio 取当前 GetGroupRatio（真源）。
func toModelGroupOut(m modelgroup.ModelGroup, chName string) modelGroupOut {
	return modelGroupOut{
		ID:          m.ID,
		Name:        m.Name,
		Ratio:       ratio_setting.GetGroupRatio(m.Name),
		ChannelID:   m.ChannelID,
		ChannelName: chName,
		Description: m.Description,
		Enabled:     m.Enabled,
		Sort:        m.Sort,
		CreatedAt:   isoUTC(m.CreatedAt),
		UpdatedAt:   isoUTC(m.UpdatedAt),
	}
}

// translateModelGroupErr 把仓储错误翻译为端点错误码（其余透传）。
func translateModelGroupErr(err error) error {
	switch {
	case errors.Is(err, modelgroup.ErrNotFound):
		return errModelGroupNotFound
	case errors.Is(err, modelgroup.ErrNameTaken):
		return errModelGroupNameTaken
	default:
		return err
	}
}

// modelGroupChannelIDs 提取去重后的非空 channel_id 集（供批量回查渠道名）。
func modelGroupChannelIDs(rows []modelgroup.ModelGroup) []int64 {
	seen := make(map[int64]struct{}, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, m := range rows {
		if m.ChannelID == nil {
			continue
		}
		id := *m.ChannelID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// channelNameOf 取渠道名（无绑定 / 未命中给空）。
func channelNameOf(channelID *int64, names map[int64]string) string {
	if channelID == nil {
		return ""
	}
	return names[*channelID]
}

// channelNamesByIDs 批量回查 new-api channels 表的 id→name（只读、单次 IN，避免 N+1）。
// 走共享 *gorm.DB 原始查询，不引入 new-api model 包耦合；失败/缺失一律给空（渠道名非关键字段）。
func (a *App) channelNamesByIDs(ctx context.Context, ids []int64) map[int64]string {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID   int64
		Name string
	}
	if err := a.DB.WithContext(ctx).
		Table("channels").Select("id, name").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out
}
