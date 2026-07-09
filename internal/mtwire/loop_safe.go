package mtwire

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// safeLoopRun 运行一轮周期任务体 fn，并用 recover 隔离**单轮** panic。
//
// 背景（修复"后台定时任务首次 panic 即永久静默停摆"）：三个 master-only 定时任务
// （StartReconcileLoop / StartBreakageSnapshotLoop / StartAgentPlanExpiryLoop）都把整个
// `for range ticker.C { runXxxOnce() }` 放进**单个** gopool.Go 任务。gopool 的 recover 在
// 任务边界，若 runXxxOnce 内出现 nil deref / 并发 map 写 / 类型断言失败等 panic，会一路展开到
// 承载整个循环的任务闭包——被 worker 记一行 GOPOOL panic 后**任务结束**，`defer ticker.Stop()`
// 触发，周期作业从此永久静默停摆（进程不崩，作业死掉，无人重新提交、无健康告警）。尤以支付卡单
// 对账停摆最危险：已付未回调的用户永不被补入账。
//
// 把每一轮调用包一层 safeLoopRun 后，单轮 panic 仅记一行错误日志、绝不上抛，循环得以存活到下一轮。
// name 用于日志区分是哪个 loop。fn 自身仍应各自做幂等/atomic 防重入（本函数只负责 panic 隔离）。
func safeLoopRun(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("mtwire: %s loop iteration panic recovered: %v", name, r))
		}
	}()
	fn()
}
