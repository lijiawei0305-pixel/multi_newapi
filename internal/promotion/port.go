package promotion

import "context"

// --- 对外接口（detailed-design §2.9 的 Go 签名）---

// PromotionService 提供推广渠道创建与注册归属。
type PromotionService interface {
	// CreateChannel 为租户（代理）创建推广渠道：前缀唯一，生成 <prefix>_<rand> 渠道码与
	// 专属注册链接 /sign-up?channel=<channel_code>。前缀重复 -> ErrChannelPrefixDup；
	// 前缀格式非法 -> ErrChannelPrefixInvalid。
	CreateChannel(ctx context.Context, tenantID int64, name, prefix string) (*Channel, error)
	// AttributeOnSignup 在用户注册时按 channelCode 绑定其归属的 tenant+channel，并令该渠道
	// registered_count+1。未知渠道码 -> ErrChannelNotFound。
	AttributeOnSignup(ctx context.Context, channelCode string, userID int64) error
	// VoidChannelsByTenant 作废某租户名下的全部推广渠道（代理升级为独立档 level>=1 时自动调用，
	// 见 mtwire.HandleAdminUpdateAgent）。作废后的渠道不再向*新*注册归属（调用方按 Channel.Voided
	// 跳过并回落 Host/none）；已归属该渠道的历史用户不受影响。幂等：无渠道 / 已全部作废的租户
	// 重复调用不报错、无副作用（再次升档 / 重复请求安全）。
	VoidChannelsByTenant(ctx context.Context, tenantID int64) error
}

// --- 消费者定义的依赖接口（本包声明，main 装配具体实现）---

// PromotionRepo 是推广渠道与归属的持久化抽象。本轮提供内存假实现（MemRepo）；
// 真实 GORM 实现（迁移 + prefix 唯一约束 + scopeByTenant + 原子 registered_count++）顺延（见报告 TODO）。
//
// 注：detailed-design §2.9 还列出对 TenantService 的依赖（建渠道时校验租户存在/有效、
// 以及“经代理域名注册→归属对应代理”的 Host→tenant 解析）。该依赖不在本轮范围内，
// 以消费者接口形式接入顺延（见报告 TODO）。
type PromotionRepo interface {
	// CreateChannel 入库并回填 c.ID；prefix 冲突返回 ErrChannelPrefixDup。
	CreateChannel(ctx context.Context, c *Channel) error
	// GetChannelByCode 按完整 channel_code 查渠道；未找到返回 ErrChannelNotFound。
	GetChannelByCode(ctx context.Context, code string) (*Channel, error)
	// IncrRegisteredCount 原子地令渠道 registered_count+1；渠道不存在返回 ErrChannelNotFound。
	IncrRegisteredCount(ctx context.Context, channelID int64) error
	// CreateAttribution 落库一条用户归属记录（user -> tenant+channel）。
	CreateAttribution(ctx context.Context, a *Attribution) error
	// VoidChannelsByTenant 原子地将某租户的全部渠道标记为已作废（voided=true）。
	// 幂等：无渠道 / 已全部作废的租户调用不报错（受影响行数可为 0，不视为失败）。
	VoidChannelsByTenant(ctx context.Context, tenantID int64) error
}
