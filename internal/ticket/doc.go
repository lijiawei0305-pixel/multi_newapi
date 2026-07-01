// Package ticket 是「支持工单」领域模块（用户提交问题 → 代理/管理员处理）。
//
// 分层与 internal/moderation、internal/wallet 一致：model（领域类型）+ port（消费者定义接口）
// + service（状态流转与校验，纯业务，不依赖 gin/gorm）+ errors（TICKET_* 错误码）+ repo（内存假实现，
// 单测替身）。真实持久化见 gormrepo 子包（support_tickets + support_ticket_messages 两表）。
//
// 命名刻意用 SupportTicket，避开既有 tokenplan.PurchaseTicket（购买凭据），二者语义无关。
//
// 三端隔离（最高优先级，见 doc/detailed-design.md 与任务卡）：
//   - 用户端：仅按会话 user_id 过滤（TicketRepo.*ForUser 强制 WHERE user_id=?）；
//   - 代理端：仅按权威 tenant_id 过滤（TicketRepo.*ForTenant 强制 WHERE tenant_id=?）；
//   - 管理端：跨租户（TicketRepo.*ForAdmin，受 AdminAuth 保护，不注入租户约束）。
//
// 跨作用域访问一律坍缩为 ErrNotFound（IDOR 防线，与 moderation scopeByTenant 同口径）。
// 每次状态变更 / 回复 / 关闭 / 重开都在 service 层重新校验归属，绝不仅在列表查询过滤。
package ticket
