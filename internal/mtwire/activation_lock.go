package mtwire

import "sync"

// 代理套餐激活的「按 owner 串行化」条带锁（见 ActivatePaidAgentPlanOrder）。
//
// 为什么需要：支付平台常在秒级并发重推同一笔回调。若两个回调都读到订单 pending、都进入
// provisionAgentFromOrder,对「新代理」两者都会 TenantService.Create(同一 owner 派生的同一 slug)。
// tenants.slug 有唯一约束(idx_tenants_slug)兜底,不会产生孤儿租户,但输家会撞唯一键报错 +
// 触发平台无谓重试。按 owner 串行化后,同一 owner 的激活同一时刻至多一个在跑,其余在临界区内
// 重读到 activated 即幂等短路,杜绝并发双 provision。
//
// 为什么按 owner 而非 orderNo：新代理的建租户冲突点是「owner 派生的 slug」,同一 owner 的**不同**
// 订单并发激活也会撞同一 slug;按 owner 串行一并覆盖(finding 建议「对 owner 加去重锁」)。
//
// 为什么进程内锁够用：部署为单实例(见 CLAUDE.md 单栈),回调均落同一进程;跨实例场景由
// tenants.slug 唯一键作最终兜底。固定条带 → 内存有界、无需清理(避免 per-key map 无限增长);
// 同一 ownerID 恒落同一条带 → 必串行,不同 owner 偶尔共用一条带 → 仅无害的短暂串行。
const agentActivationLockStripes = 256

var agentActivationLocks [agentActivationLockStripes]sync.Mutex

// lockAgentActivation 锁定 ownerID 所属条带,返回解锁函数(供 defer 调用)。
func lockAgentActivation(ownerID int64) func() {
	idx := uint64(ownerID) % agentActivationLockStripes
	m := &agentActivationLocks[idx]
	m.Lock()
	return m.Unlock
}
