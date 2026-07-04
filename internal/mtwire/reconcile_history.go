package mtwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// reconcileRunRow 一条对账运行记录（reconcile_runs 表）：手动全记 + 定时有实事才记，供 admin 历史查询。
type reconcileRunRow struct {
	ID      int64     `gorm:"column:id;primaryKey;autoIncrement"`
	RanAt   time.Time `gorm:"column:ran_at;index"`
	Trigger string    `gorm:"column:trigger;type:varchar(8);not null"` // cron | manual
	Summary string    `gorm:"column:summary;type:varchar(255);not null"`
	Detail  string    `gorm:"column:detail;type:text"` // JSON: paid/created/sub 三路 order_no 明细
}

// TableName 固定表名。
func (reconcileRunRow) TableName() string { return "reconcile_runs" }

// reconcileHeartbeatRow 单行心跳（reconcile_heartbeat 表，固定 id=1）：每轮对账 upsert，供 admin
// 顶部心跳条常显「最近对账时间 / 今日轮次 / 上轮卡单·失败数」，解决健康系统历史为空看不出在跑的困惑。
type reconcileHeartbeatRow struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement:false"` // 固定 1
	LastRunAt       time.Time `gorm:"column:last_run_at"`
	LastTrigger     string    `gorm:"column:last_trigger;type:varchar(8)"`
	TodayDate       string    `gorm:"column:today_date;type:varchar(10)"` // YYYY-MM-DD，跨天重置 today_runs
	TodayRuns       int       `gorm:"column:today_runs;not null;default:0"`
	LastStuckCount  int       `gorm:"column:last_stuck_count;not null;default:0"`
	LastFailedCount int       `gorm:"column:last_failed_count;not null;default:0"`
}

// TableName 固定表名。
func (reconcileHeartbeatRow) TableName() string { return "reconcile_heartbeat" }

func migrateReconcileRuns(db *gorm.DB) error      { return db.AutoMigrate(&reconcileRunRow{}) }
func migrateReconcileHeartbeat(db *gorm.DB) error { return db.AutoMigrate(&reconcileHeartbeatRow{}) }

// reconcileHasFacts 报告一轮对账是否「有实事」（任一路径扫到单，或任一路径有失败）——决定定时轮是否落史。
// （手动触发另行总记，不看此函数。）避免每 5min 空跑刷屏。
func reconcileHasFacts(paid, created payment.ReconcileResult, sub ReconcileSubResult) bool {
	return paid.Scanned > 0 || created.Scanned > 0 || sub.Scanned > 0 ||
		len(paid.Failed) > 0 || len(created.Failed) > 0 || len(sub.Failed) > 0 ||
		len(created.Expired) > 0
}

// recordReconcileRun 落一条对账历史（best-effort：写库失败仅放弃记录，绝不影响对账本身）。
func (a *App) recordReconcileRun(ctx context.Context, trigger string, paid, created payment.ReconcileResult, sub ReconcileSubResult) {
	summary := fmt.Sprintf("RCG-paid 扫%d/补%d/败%d · RCG-created 扫%d/补%d/过期%d/败%d · SUB 扫%d/激活%d/未付%d/败%d",
		paid.Scanned, len(paid.Reconciled), len(paid.Failed),
		created.Scanned, len(created.Reconciled), len(created.Expired), len(created.Failed),
		sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Failed))
	detail, _ := json.Marshal(map[string]any{"paid": paid, "created": created, "sub": sub})
	row := &reconcileRunRow{RanAt: time.Now(), Trigger: trigger, Summary: summary, Detail: string(detail)}
	_ = a.DB.WithContext(ctx).Create(row).Error
}

// listReconcileRuns 取最近 limit 条对账记录（按 ran_at 倒序）。
func (a *App) listReconcileRuns(ctx context.Context, limit int) ([]reconcileRunRow, error) {
	var rows []reconcileRunRow
	err := a.DB.WithContext(ctx).Order("ran_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// updateReconcileHeartbeat 每轮对账 upsert 单行心跳（best-effort）；today_runs 跨天自动重置。
func (a *App) updateReconcileHeartbeat(ctx context.Context, trigger string, stuckCount, failedCount int) {
	now := time.Now()
	today := now.Format("2006-01-02")
	var hb reconcileHeartbeatRow
	err := a.DB.WithContext(ctx).Take(&hb, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		hb = reconcileHeartbeatRow{
			ID: 1, LastRunAt: now, LastTrigger: trigger, TodayDate: today, TodayRuns: 1,
			LastStuckCount: stuckCount, LastFailedCount: failedCount,
		}
		_ = a.DB.WithContext(ctx).Create(&hb).Error
		return
	}
	if err != nil {
		return // best-effort：读失败即放弃本轮心跳，不影响对账
	}
	if hb.TodayDate != today {
		hb.TodayDate = today
		hb.TodayRuns = 0
	}
	hb.TodayRuns++
	hb.LastRunAt = now
	hb.LastTrigger = trigger
	hb.LastStuckCount = stuckCount
	hb.LastFailedCount = failedCount
	_ = a.DB.WithContext(ctx).Save(&hb).Error
}

// getReconcileHeartbeat 读单行心跳；无记录（从未跑过）返回 (zero, false)。
func (a *App) getReconcileHeartbeat(ctx context.Context) (reconcileHeartbeatRow, bool) {
	var hb reconcileHeartbeatRow
	if err := a.DB.WithContext(ctx).Take(&hb, "id = ?", 1).Error; err != nil {
		return reconcileHeartbeatRow{}, false
	}
	return hb, true
}
