// Package risk 是横切的风控模块：调用前状态/限流/IP 校验、Trial 限购、满额逼近告警。
//
// 设计依据 doc/detailed-design.md §2.13、doc/tasks/13-risk.md、doc/proposal.md §2.4/§13。
// 对外接口 RiskEngine（CheckCall / CheckPurchaseLimit / NoteUsage）由 *Engine 实现；
// 一切依赖（KVCache、PurchaseLedger、Clock、StatusChecker、IPAllowlist、RPMResolver、AlertSink）
// 均为本包声明的消费者接口，运行时由 mtwire 注入（detailed-design §1.4）。
//
// 限购权威（C4）：PurchaseLedger（表 risk_purchase_claims，见 gormrepo）为持久台账；
// KVCache（Redis/Mem）仅为多副本加速镜像。Redis 抹掉后历史 Trial 占用仍以 DB 为准拒绝。
// 无 ledger 时退回纯 KV（兼容既有单测）；生产 wire 必须 WithPurchaseLedger。
package risk
