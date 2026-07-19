package mtwire

// billingWriter：钱包消耗展示台账的异步批处理器（AGENT_HOOK_ASYNC_ENABLED 开启才生效）。
//
// 背景（发现 #9 / 写放大）：上游 BATCH_UPDATE 只批量 5 类原生计数；本 fork 追加的两类自研写不在其覆盖内——
// mt_wallet_consume_log 的 INSERT（display-only 钱包消耗台账，recordWalletConsume）。该台账不产生收益、
// 不改变余额，以 requestID 幂等，适合批量写入并在失败后重试。真实可提现收益 agent_earning_logs +
// agent_wallets 明确不进入本进程缓冲：creditEarning 必须在请求路径同步提交，进程崩溃不能吞掉待付收益。
//
// 本 writer 按 interval 定时（或达阈值主动）多行 INSERT ... ON CONFLICT DO NOTHING，把 N 次展示台账
// INSERT 合并为批次。失败批次回填队首等待下一轮；缓冲打满时新行同步兜底，防止无界增长。

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	billingFlushThreshold = 256             // 缓冲达此量级即主动 flush（不干等 ticker），削峰降尾延迟
	billingHardCap        = 8192            // 缓冲硬上限：达到即同步兜底写，防 flush 停滞（DB 异常）下无界增长
	billingConsumeBatch   = 500             // 消耗台账多行 INSERT 的单语句批大小
	billingShutdownWait   = 8 * time.Second // 优雅关闭 flush 最长等待（< docker 默认 10s SIGTERM grace）
)

// billingWriter 缓冲展示台账并定时批量落库。buffer 经 mu 保护；db 在构造后只读。
type billingWriter struct {
	db *gorm.DB

	interval time.Duration

	mu          sync.Mutex
	consumeRows []walletConsumeRow

	wake      chan struct{} // 缓冲达阈值时主动唤醒 flush（非阻塞 poke；flush 恒在 run goroutine 上，绝不占用请求 goroutine）
	stopCh    chan struct{}
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// newBillingWriter 构造（不启动）writer。interval<=0 兜底 2s。
func newBillingWriter(db *gorm.DB) *billingWriter {
	interval := time.Duration(common.AgentHookAsyncInterval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &billingWriter{
		db:       db,
		interval: interval,
		wake:     make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// start 启动 flush 循环 + SIGTERM 优雅 flush 钩子（幂等，sync.Once）。仅由 StartBillingWriter 在 flag 开启时调用。
func (w *billingWriter) start() {
	w.startOnce.Do(func() {
		gopool.Go(w.run)
		w.installSignalFlush()
		common.SysLog(fmt.Sprintf("mtwire: agent hook async billing writer started (interval=%ds)", int(w.interval/time.Second)))
	})
}

// enqueueConsume 缓冲一条钱包消耗台账；打满则同步兜底写（不丢、不无界）。调用方已保证 walletQuota/tenant/requestID 有效。
func (w *billingWriter) enqueueConsume(row walletConsumeRow) {
	w.mu.Lock()
	if len(w.consumeRows) >= billingHardCap {
		w.mu.Unlock()
		w.syncInsertConsume(row)
		return
	}
	w.consumeRows = append(w.consumeRows, row)
	poke := len(w.consumeRows) >= billingFlushThreshold
	w.mu.Unlock()
	if poke {
		w.pokeFlush()
	}
}

func (w *billingWriter) pokeFlush() {
	select {
	case w.wake <- struct{}{}:
	default: // 已有待处理唤醒信号，无需重复
	}
}

// run 是 flush 主循环：ticker 定时 + 阈值唤醒；每轮经 safeLoopRun 隔离单轮 panic（对齐既有 loop 纪律）。
// 收到 stop 先做一次收尾 flush 再退出（优雅关闭 / 计划重启不丢缓冲）。
func (w *billingWriter) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			safeLoopRun("billing-writer-final", w.flush)
			close(w.doneCh)
			return
		case <-w.wake:
			safeLoopRun("billing-writer", w.flush)
		case <-ticker.C:
			safeLoopRun("billing-writer", w.flush)
		}
	}
}

// flush 把当前缓冲一次性落库。先在锁内换出缓冲（释放锁后再做 DB IO，绝不持锁跨 IO），失败则限量回填重排
// （幂等键保证重排不双计），回填超硬上限丢最新、留最旧的失败记账（方向安全）。
func (w *billingWriter) flush() {
	w.mu.Lock()
	consume := w.consumeRows
	w.consumeRows = nil
	w.mu.Unlock()

	if len(consume) > 0 {
		if err := w.db.WithContext(context.Background()).
			Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(consume, billingConsumeBatch).Error; err != nil {
			common.SysError("mtwire: billing writer flush consume-log failed (requeue): " + err.Error())
			w.requeueConsume(consume)
		}
	}
}

// syncInsertConsume 是台账的同步兜底写（缓冲打满时），与 recordWalletConsume 关闭异步时的逐条写完全一致。
func (w *billingWriter) syncInsertConsume(row walletConsumeRow) {
	if err := w.db.WithContext(context.Background()).
		Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		common.SysError("mtwire: billing writer consume sync-fallback failed: " + err.Error())
	}
}

// requeueConsume 把 flush 失败的台账回填到缓冲头部（优先重试），超硬上限丢队尾（最新）。
func (w *billingWriter) requeueConsume(rows []walletConsumeRow) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.consumeRows = append(rows, w.consumeRows...)
	if over := len(w.consumeRows) - billingHardCap; over > 0 {
		w.consumeRows = w.consumeRows[:billingHardCap]
		common.SysError(fmt.Sprintf("mtwire: billing writer consume-log buffer over cap, dropped %d rows", over))
	}
}

// installSignalFlush 装 SIGTERM/SIGINT 钩子：收到即先 flush（≤billingShutdownWait），再复发信号交默认动作让进程照常退出。
// 这是全进程唯一的信号处理（现有代码无优雅关闭），故拦截后必须复发，否则容器只能等到 SIGKILL 才死。
func (w *billingWriter) installSignalFlush() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	gopool.Go(func() {
		s := <-sig
		common.SysLog("mtwire: shutdown signal received, flushing async billing buffer")
		w.close()
		signal.Stop(sig)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(s) // 复发：默认动作终结进程（flush 已完成）
		}
	})
}

// close 触发收尾 flush 并等 run 退出（超时仍返回，绝不吊死关闭流程）。幂等（sync.Once）。
func (w *billingWriter) close() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	select {
	case <-w.doneCh:
	case <-time.After(billingShutdownWait):
		common.SysError("mtwire: billing writer shutdown flush timed out")
	}
}
