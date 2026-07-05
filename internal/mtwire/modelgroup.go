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
	"gorm.io/gorm"

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
	// 层级轴（Change 1，spec §9.6.1）：非「可代理覆盖层级」或用户不归属 L1 代理 → 平台全局
	// groupRatioOf(userGroup)（既有行为不变）；归属 L1 代理 → 该代理自设的 per-tenant 覆盖（未配置则 1，
	// 不回退平台全局，"No overlap"）。见 resolveTierRatio（grouphook.go）。
	tier := a.resolveTierRatio(context.Background(), userID, userGroup)
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

// servingChannel 是「服务渠道」只读视图：某模型分组被哪个 new-api 渠道服务（id+名）。
type servingChannel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// modelGroupOut 是模型分组管理列表/详情 DTO。ratio 取自当前 GetGroupRatio（真源），非本表列。
//
// serving_channels：只读「服务渠道」列表，反向推导自渠道侧——所有「channels.group 含本分组名」的渠道。
// 渠道↔模型分组是多对一（一个分组可被多个渠道服务），绑定真源在渠道的「模型分组」字段，本表不再持有 channel_id。
type modelGroupOut struct {
	ID              int64            `json:"id"`
	Name            string           `json:"name"`
	Ratio           float64          `json:"ratio"`
	ServingChannels []servingChannel `json:"serving_channels"`
	Description     string           `json:"description"`
	Enabled         bool             `json:"enabled"`
	Sort            int              `json:"sort"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

// modelGroupCreateIn 是 POST 入参。enabled 缺省 true。绑定不在此设——由渠道侧 channels.group 决定。
type modelGroupCreateIn struct {
	Name        string  `json:"name"`
	Ratio       float64 `json:"ratio"`
	Description string  `json:"description"`
	Enabled     *bool   `json:"enabled"`
	Sort        int     `json:"sort"`
}

// modelGroupUpdateIn 是 PUT 局部更新入参（指针字段，仅传则改）。无 channel_id：绑定由渠道侧决定。
type modelGroupUpdateIn struct {
	Ratio       *float64 `json:"ratio"`
	Description *string  `json:"description"`
	Enabled     *bool    `json:"enabled"`
	Sort        *int     `json:"sort"`
}

// HandleAdminListModelGroups GET /api/admin/model-groups —— 列出全部模型分组（含当前倍率 + 服务渠道）。需 AdminAuth。
func (a *App) HandleAdminListModelGroups(c *gin.Context) {
	ctx := reqCtx(c)
	rows, err := a.ModelGroupRepo.List(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	serving := a.servingChannelsByGroup(ctx, modelGroupNames(rows))
	out := make([]modelGroupOut, 0, len(rows))
	for _, m := range rows {
		out = append(out, toModelGroupOut(m, serving[m.Name]))
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
	// 新建后服务渠道由渠道侧决定（前端创建成功即刷新列表回查），响应给空列表即可。
	respondOK(c, toModelGroupOut(*mg, nil))
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
	// ① 更新 model_groups 元数据（description/enabled/sort）。绑定不在此改——由渠道侧 channels.group 决定。
	if err := a.ModelGroupRepo.Update(ctx, name, modelgroup.ModelGroupUpdate{
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

// ReconcileModelGroupsUsable 启动自愈（master 幂等）：确保每个 enabled 模型分组都在 UserUsableGroups
// （缺则补，label 取 description|name）与 GroupRatio（缺则补占位 1.0、不覆盖已有）里——使「加模型分组即处处
// 可用」不依赖创建路径（管理页/手工插库/seed 皆自愈）。档位（default/vip/svip 等**非** model_groups 组）不在
// 此表 → 永不会被加进 UserUsableGroups → 用户不可自选档位（防越权，见 seed.go）仍成立。仅在有缺失时写库。
//
// 修复背景：早先手工/旁路建入的模型分组只进了 GroupRatio+model_groups、漏了 UserUsableGroups，导致用户建 Key
// 只能选 default → 够不到该模型分组（gemini「no available channel under group default」根因）。本自愈一并补齐。
func (a *App) ReconcileModelGroupsUsable() error {
	if a.ModelGroupRepo == nil {
		return nil
	}
	rows, err := a.ModelGroupRepo.List(context.Background())
	if err != nil {
		return err
	}
	gr := ratio_setting.GetGroupRatioCopy()
	uug := setting.GetUserUsableGroupsCopy()
	changed := false
	for _, m := range rows {
		if !m.Enabled {
			continue
		}
		if _, ok := gr[m.Name]; !ok {
			gr[m.Name] = 1 // 占位倍率（无折扣）；实际倍率由模型分组管理页设定，此处只补缺不覆盖
			changed = true
		}
		if _, ok := uug[m.Name]; !ok {
			label := strings.TrimSpace(m.Description)
			if label == "" {
				label = m.Name
			}
			uug[m.Name] = label
			changed = true
		}
	}
	if !changed {
		return nil
	}
	grJSON, err := json.Marshal(gr)
	if err != nil {
		return err
	}
	uugJSON, err := json.Marshal(uug)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		"GroupRatio":       string(grJSON),
		"UserUsableGroups": string(uugJSON),
	})
}

// ============================================================================
// 辅助
// ============================================================================

// toModelGroupOut 映射 DTO；ratio 取当前 GetGroupRatio（真源）。serving 为反推的服务渠道（nil→空列表，保证 JSON 出 []）。
func toModelGroupOut(m modelgroup.ModelGroup, serving []servingChannel) modelGroupOut {
	if serving == nil {
		serving = []servingChannel{}
	}
	return modelGroupOut{
		ID:              m.ID,
		Name:            m.Name,
		Ratio:           ratio_setting.GetGroupRatio(m.Name),
		ServingChannels: serving,
		Description:     m.Description,
		Enabled:         m.Enabled,
		Sort:            m.Sort,
		CreatedAt:       isoUTC(m.CreatedAt),
		UpdatedAt:       isoUTC(m.UpdatedAt),
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

// modelGroupNames 提取登记表里的全部分组名（供批量反推服务渠道）。
func modelGroupNames(rows []modelgroup.ModelGroup) []string {
	names := make([]string, 0, len(rows))
	for _, m := range rows {
		names = append(names, m.Name)
	}
	return names
}

// servingChannelsByGroup 反向推导每个模型分组的「服务渠道」：所有「channels.group（逗号分隔）含该分组名」的渠道。
//
// 绑定真源在渠道侧（渠道的「模型分组」字段 channels.group），渠道↔模型分组是多对一——一个分组可被多个渠道服务，
// 故本表 model_groups 不再持有单一 channel_id。这里单次全量扫 channels(id,name,group) 后在内存按分组名归集
// （渠道量级小，避免 N+1 / 每组一次 LIKE），并对每个 group 段去空白（比 MySQL FIND_IN_SET 更稳健，兼容 "a, b" 写法）。
// 只读、失败给空（服务渠道是展示字段，非计费关键路径）。channels 表无软删列，故无需排除 deleted。
func (a *App) servingChannelsByGroup(ctx context.Context, groupNames []string) map[string][]servingChannel {
	out := make(map[string][]servingChannel, len(groupNames))
	if len(groupNames) == 0 {
		return out
	}
	want := make(map[string]struct{}, len(groupNames))
	for _, n := range groupNames {
		want[n] = struct{}{}
	}
	var rows []struct {
		ID    int64
		Name  string
		Group string `gorm:"column:group"`
	}
	// SELECT id, name, `group`（group 为 SQL 保留字，按方言加引号）；按 id 升序输出稳定。
	if err := a.DB.WithContext(ctx).
		Table("channels").
		Select("id, name, " + channelGroupColumn(a.DB)).
		Order("id asc").
		Find(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		for _, g := range strings.Split(r.Group, ",") {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if _, ok := want[g]; ok {
				out[g] = append(out[g], servingChannel{ID: r.ID, Name: r.Name})
			}
		}
	}
	return out
}

// channelGroupColumn 返回按数据库方言正确加引号的 channels.group 列名（group 为 SQL 保留字）。
// 与 new-api model 包 initCol 保持一致：MySQL 用反引号、其余（sqlite/postgres）用双引号。
func channelGroupColumn(db *gorm.DB) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "mysql" {
		return "`group`"
	}
	return `"group"`
}
