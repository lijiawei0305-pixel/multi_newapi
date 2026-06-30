package moderation

import (
	"context"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// --- 对外接口（detailed-design §2.14 的 Go 签名）---

// Moderator 在转发前扫描用户输入消息，判定是否命中违禁词并给出动作与提醒。
type Moderator interface {
	// ScanUserMessages 扫描本次请求的用户消息（已抽取为纯文本）。
	// 合并「全站基础库(tenant_id=0) ∪ p.TenantID 自有词」后匹配；命中→Hit=true，
	// Matches 为脱敏命中词，Action 取所有命中词中最严者（block > remind），
	// Reminder 为面向用户的提醒文案。未命中→返回 Hit=false 的零值结果（非 nil）。
	//
	// 本方法只做判定，不负责记录违规——由调用方据结果调用 ViolationSink.Record，
	// 以便在 relay 层一并带上 token_id/model 等本方法不感知的上下文。
	ScanUserMessages(ctx context.Context, p *appctx.Principal, msgs []Message) (*ModerationResult, error)
}

// --- 消费者定义的依赖接口（本包声明，main 装配具体实现）---

// Matcher 是违禁词匹配引擎抽象（本模块内置 Aho-Corasick 实现，见 matcher.go）。
// 与具体匹配库解耦，便于单测注入与替换。实现需对 text 与词做统一归一（大小写/全半角）。
type Matcher interface {
	// Match 用给定规则集扫描 text，返回命中的规则（仅含 Enabled 的判定由上层保证）。
	// 同一规则至多返回一次；返回空切片表示未命中。
	Match(text string, words []BannedWord) []BannedWord
}

// BannedWordRepo 是违禁词库的持久化抽象。
type BannedWordRepo interface {
	// ListWords 返回 tenant_id 恰等于 tenantID 的全部词（含禁用，供 CRUD 展示与扫描合并）。
	// 扫描时由 service 分别取 ListWords(0) 与 ListWords(tenantID) 合并。
	ListWords(ctx context.Context, tenantID int64) ([]BannedWord, error)
	// UpsertWord 新增或更新一条违禁词：w.ID=0 为新增并回填 w.ID，否则按 ID 更新。
	// 词非法（空/超长/正则编译失败）返回 ErrWordInvalid。
	UpsertWord(ctx context.Context, w *BannedWord) error
	// DeleteWord 删除某租户名下指定违禁词；跨租户删除视为不存在（scopeByTenant）→ ErrWordNotFound。
	DeleteWord(ctx context.Context, tenantID, id int64) error
}

// ViolationSink 记录违规事件并供管理端查阅。
type ViolationSink interface {
	// Record 落库一条违规事件（ev.ID 由实现回填）。
	Record(ctx context.Context, ev *ViolationEvent) error
	// ListForAdmin 按租户 + 过滤条件查违规日志（按 created_at 倒序）。
	ListForAdmin(ctx context.Context, tenantID int64, f ViolationFilter) ([]ViolationEvent, error)
}
