package promotion

import "time"

// Channel 是推广渠道实体（对应 proposal §6 `agent_promotion_channels` 表）。
// 一期核心字段：prefix / channel_code / signup_url / registered_count。
type Channel struct {
	ID              int64
	TenantID        int64  // 归属代理（租户）
	Name            string // 渠道展示名（如“微信公众号”）
	Prefix          string // 业务唯一前缀（如 "wechat"）
	ChannelCode     string // 完整渠道码 <prefix>_<rand>
	SignupURL       string // 专属注册链接 /sign-up?channel=<channel_code>
	RegisteredCount int64  // 经本渠道注册的用户数
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Attribution 是“用户经链接/域名注册 → 归属代理 + 渠道”的绑定记录。
// 真实实现中通常落在用户 / `tenant_users` 行的 tenant_id + promotion_channel_id 字段上
// （而非独立表），本轮以独立记录表达绑定语义（见报告 TODO）。
type Attribution struct {
	UserID      int64
	TenantID    int64
	ChannelID   int64
	ChannelCode string
	CreatedAt   time.Time
}
