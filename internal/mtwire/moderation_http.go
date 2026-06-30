package mtwire

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/moderation"
)

// --- DTO（snake_case）---

type moderationWordInput struct {
	ID        int64  `json:"id"`
	Word      string `json:"word"`
	MatchType string `json:"match_type"`
	Action    string `json:"action"`
	Enabled   *bool  `json:"enabled"` // nil = 默认启用
}

type moderationWordOutput struct {
	ID        int64  `json:"id"`
	TenantID  int64  `json:"tenant_id"`
	Word      string `json:"word"`
	MatchType string `json:"match_type"`
	Action    string `json:"action"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

type violationOutput struct {
	ID           int64    `json:"id"`
	TenantID     int64    `json:"tenant_id"` // 主站全局视图按此列区分代理/租户
	UserID       int64    `json:"user_id"`
	Username     string   `json:"username"` // 冗余账号名，前端显示「name (id)」
	TokenID      int64    `json:"token_id"`
	Model        string   `json:"model"`
	MatchedWords []string `json:"matched_words"`
	Excerpt      string   `json:"excerpt"`
	ActionTaken  string   `json:"action_taken"`
	CreatedAt    int64    `json:"created_at"`
}

func toWordOut(w moderation.BannedWord) moderationWordOutput {
	return moderationWordOutput{
		ID:        w.ID,
		TenantID:  w.TenantID,
		Word:      w.Word,
		MatchType: string(w.MatchType),
		Action:    string(w.Action),
		Enabled:   w.Enabled,
		CreatedAt: w.CreatedAt.Unix(),
	}
}

func wordsOut(ws []moderation.BannedWord) []moderationWordOutput {
	out := make([]moderationWordOutput, 0, len(ws))
	for _, w := range ws {
		out = append(out, toWordOut(w))
	}
	return out
}

func violationsOut(evs []moderation.ViolationEvent, names map[int64]string) []violationOutput {
	out := make([]violationOutput, 0, len(evs))
	for _, e := range evs {
		out = append(out, violationOutput{
			ID:           e.ID,
			TenantID:     e.TenantID,
			UserID:       e.UserID,
			Username:     names[e.UserID],
			TokenID:      e.TokenID,
			Model:        e.Model,
			MatchedWords: e.MatchedWords,
			Excerpt:      e.Excerpt,
			ActionTaken:  string(e.ActionTaken),
			CreatedAt:    e.CreatedAt.Unix(),
		})
	}
	return out
}

// usernamesByID 批量查违规记录涉及的用户名（去重，单次 IN 查询）；查不到的留空。
func (a *App) usernamesByID(ctx context.Context, evs []moderation.ViolationEvent) map[int64]string {
	names := map[int64]string{}
	ids := make([]int64, 0, len(evs))
	for _, e := range evs {
		if _, ok := names[e.UserID]; !ok {
			names[e.UserID] = ""
			ids = append(ids, e.UserID)
		}
	}
	if len(ids) == 0 {
		return names
	}
	var rows []struct {
		ID       int64
		Username string
	}
	_ = a.DB.WithContext(ctx).Table("users").Select("id, username").Where("id IN ?", ids).Find(&rows).Error
	for _, r := range rows {
		names[r.ID] = r.Username
	}
	return names
}

// upsertModerationWord 是 admin(tenantID=0) 与 agent(本租户) 共用的新增/更新逻辑。
func (a *App) upsertModerationWord(c *gin.Context, tenantID int64) {
	var in moderationWordInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	mt := moderation.MatchType(in.MatchType)
	if mt == "" {
		mt = moderation.MatchContains
	}
	act := moderation.ModerationAction(in.Action)
	if act == "" {
		act = moderation.ActionBlock // 默认拦截（违禁词通常意在挡住；只想记录的显式选 remind）
	}
	w := &moderation.BannedWord{
		ID:        in.ID,
		TenantID:  tenantID,
		Word:      in.Word,
		MatchType: mt,
		Action:    act,
		Enabled:   enabled,
	}
	if err := a.ModerationRepo.UpsertWord(reqCtx(c), w); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toWordOut(*w))
}

func violationFilterFromQuery(c *gin.Context) moderation.ViolationFilter {
	f := moderation.ViolationFilter{Limit: 100}
	if v := c.Query("user_id"); v != "" {
		f.UserID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("limit"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 && n <= 500 {
			f.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 {
			f.Offset = n
		}
	}
	if v := c.Query("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	if v := c.Query("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = t
		}
	}
	return f
}

// --- admin：全站基础库（tenant_id=0）+ 违规日志（当前 Host 租户）---

// HandleAdminListModerationWords 列出全站基础库违禁词。
func (a *App) HandleAdminListModerationWords(c *gin.Context) {
	ws, err := a.ModerationRepo.ListWords(reqCtx(c), 0)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, wordsOut(ws))
}

// HandleAdminUpsertModerationWord 新增/更新全站基础库违禁词。
func (a *App) HandleAdminUpsertModerationWord(c *gin.Context) { a.upsertModerationWord(c, 0) }

// HandleAdminDeleteModerationWord 删除全站基础库违禁词。
func (a *App) HandleAdminDeleteModerationWord(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := a.ModerationRepo.DeleteWord(reqCtx(c), 0, id); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"deleted": true})
}

// HandleAdminListViolations 查「全租户」违规日志（主站统一管控，输出含 tenant_id）。
// 仅 super-admin 经 AdminAuth 可达；按 user_id/since/until/limit/offset 过滤。
func (a *App) HandleAdminListViolations(c *gin.Context) {
	evs, err := a.ModerationRepo.ListForAdmin(reqCtx(c), -1, violationFilterFromQuery(c)) // -1 = 全租户
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, violationsOut(evs, a.usernamesByID(reqCtx(c), evs)))
}

// --- agent：本租户词库（AgentOwnerAuth）---

// HandleAgentListModerationWords 列出本租户违禁词。
func (a *App) HandleAgentListModerationWords(c *gin.Context) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ws, err := a.ModerationRepo.ListWords(reqCtx(c), tid)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, wordsOut(ws))
}

// HandleAgentUpsertModerationWord 新增/更新本租户违禁词。
func (a *App) HandleAgentUpsertModerationWord(c *gin.Context) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	a.upsertModerationWord(c, tid)
}

// HandleAgentDeleteModerationWord 删除本租户违禁词（跨租户视为不存在）。
func (a *App) HandleAgentDeleteModerationWord(c *gin.Context) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := a.ModerationRepo.DeleteWord(reqCtx(c), tid, id); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"deleted": true})
}

// HandleAgentListBaseWords 让代理「只读」查看全站基础库（继承的全局词，tenant_id=0）。
func (a *App) HandleAgentListBaseWords(c *gin.Context) {
	if agentTenantID(c) <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ws, err := a.ModerationRepo.ListWords(reqCtx(c), 0)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, wordsOut(ws))
}

// HandleAgentListViolations 让代理查「本租户」的违规日志（scopeByTenant）。
func (a *App) HandleAgentListViolations(c *gin.Context) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	evs, err := a.ModerationRepo.ListForAdmin(reqCtx(c), tid, violationFilterFromQuery(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, violationsOut(evs, a.usernamesByID(reqCtx(c), evs)))
}
