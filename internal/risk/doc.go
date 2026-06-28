// Package risk 是横切的风控模块：调用前状态/限流/IP 校验、Trial 限购、满额逼近告警。
//
// 设计依据 doc/detailed-design.md §2.13、doc/tasks/13-risk.md、doc/proposal.md §2.4/§13。
// 对外接口 RiskEngine（CheckCall / CheckPurchaseLimit / NoteUsage）由 *Engine 实现；
// 一切依赖（KVCache、Clock、StatusChecker、IPAllowlist、RPMResolver、AlertSink）均为本包声明的
// 消费者接口，运行时由 cmd/main 注入实现（detailed-design §1.4：不 import 兄弟业务模块）。
//
// 本轮提供纯逻辑 + 内存假实现（MemKVCache 等）+ 并发单测；真实 Redis、GORM RiskRepo、
// Gin handler 顺延（见报告 TODO）。
package risk
