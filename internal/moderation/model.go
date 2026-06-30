package moderation

import "time"

// MatchType 违禁词匹配方式。
type MatchType string

const (
	// MatchContains 子串包含（默认；大词库走 Aho-Corasick 多模匹配）。
	MatchContains MatchType = "contains"
	// MatchExact 整条消息文本精确等于（归一后比较）。
	MatchExact MatchType = "exact"
	// MatchRegex 正则匹配（每词独立编译）。
	MatchRegex MatchType = "regex"
)

// ModerationAction 命中违禁词后的处置动作。
type ModerationAction string

const (
	// ActionRemind 放行 + 提醒（默认）。
	ActionRemind ModerationAction = "remind"
	// ActionBlock 拦截请求。
	ActionBlock ModerationAction = "block"
)

// Message 是一条待审对话消息（仅取文本部分；多模态图片等非文本内容由调用方剔除）。
type Message struct {
	Role string
	Text string
}

// BannedWord 是一条违禁词规则。
// TenantID=0 表示全站基础库（所有租户继承）；>0 为某租户自有词。
type BannedWord struct {
	ID        int64
	TenantID  int64
	Word      string
	MatchType MatchType
	Action    ModerationAction
	Enabled   bool
	CreatedAt time.Time
}

// ModerationResult 是一次扫描的结果。
type ModerationResult struct {
	Hit      bool             // 是否命中
	Matches  []string         // 命中的违禁词（已脱敏，去重，可入库/回显）
	Action   ModerationAction // 最终动作：任一命中词为 block 即 block，否则 remind
	Reminder string           // 面向用户的提醒文案（remind/block 均可带）
}

// ViolationEvent 是一条违规记录，供管理员审阅。
type ViolationEvent struct {
	ID           int64
	TenantID     int64
	UserID       int64
	TokenID      int64
	Model        string
	MatchedWords []string // 命中词（脱敏）
	Excerpt      string   // 触发片段（脱敏截断）
	ActionTaken  ModerationAction
	CreatedAt    time.Time
}

// ViolationFilter 是违规日志的查询过滤条件（管理端）。零值字段表示不限。
type ViolationFilter struct {
	UserID int64     // 0 = 不限用户
	Since  time.Time // 零值 = 不限起始时间
	Until  time.Time // 零值 = 不限截止时间
	Limit  int       // 0 = 默认上限
	Offset int       // 分页偏移
}
