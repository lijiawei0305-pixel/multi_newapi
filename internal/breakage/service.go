/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// service.go 是 breakage.Service 门面实现：薄封装 Repo，透传租户作用域、做区间校验 +
// _usd 金额边界 round2，并在 RunSnapshot 落快照后按阈值 best-effort 触发告警（alert.AlertSink）。
//
// 分层职责（见 .ccg/tasks/breakage-monitor/plan.md L1-a）：
//   - 聚合 SQL / 快照读写：Repo（internal/breakage/gormrepo），本层只依赖接口不碰 gorm。
//   - 区间校验（跨度 > 366 天 → ErrRangeInvalid）、金额 round2（half-away-from-zero）在本层。
//   - 告警：RunSnapshot 采集+落库后，扫「到期未使用沉淀 / 系统异常卡单」按阈值 Dispatch，
//     best-effort：告警失败只影响告警本身，绝不回滚已落库的快照、不打断 job（返回落库条数）。
package breakage

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/internal/alert"
)

// maxRangeSeconds 是快照趋势 / 明细期末区间的跨度上限（366 天，含闰年裕量）；
// 超出 → ErrRangeInvalid。与 internal/mtwire/report.go maxRangeSeconds、财务报表约定同口径。
const maxRangeSeconds int64 = 366 * 24 * 60 * 60

// service 是 Service 的实现。仅依赖 Repo + alert.AlertSink 两个接口（不 import gormrepo），
// 保证升级 rebase 安全且可全 fake 单测。cfg 决定告警是否启用与阈值。
type service struct {
	repo Repo
	sink alert.AlertSink
	cfg  alert.Config
}

// 编译期确认 service 满足 Service。
var _ Service = (*service)(nil)

// NewService 装配 breakage 门面。sink 为 nil 时告警静默跳过（不 panic）；
// cfg 决定告警总开关与阈值（cfg.ThresholdPct <=0 时 RunSnapshot 回退 alert.DefaultThresholdPct）。
func NewService(repo Repo, sink alert.AlertSink, cfg alert.Config) Service {
	return &service{repo: repo, sink: sink, cfg: cfg}
}

// Overview 透传租户作用域 + now 聚合 4 指标，并对 3 个 _usd 金额做边界 round2（计数 AnomalyCount 不动）。
func (s *service) Overview(ctx context.Context, tenantID *int64, now int64) (Overview, error) {
	ov, err := s.repo.Overview(ctx, tenantID, now)
	if err != nil {
		return Overview{}, err
	}
	ov.ActiveRemainingUSD = round2(ov.ActiveRemainingUSD)
	ov.ExpiredUnusedUSD = round2(ov.ExpiredUnusedUSD)
	ov.WalletUnusedUSD = round2(ov.WalletUnusedUSD)
	return ov, nil
}

// Detail 校验 Filter（区间跨度 / 告警级合法性）后透传分页明细，并对每行 _usd 金额做边界 round2。
// 返回 (行, 匹配总数, error)。UsagePct 是比例不做金额舍入（保留 Repo 计算精度，展示端自处理）。
func (s *service) Detail(ctx context.Context, f Filter, now int64) ([]DetailRow, int64, error) {
	if err := validateRange(f.Start, f.End); err != nil {
		return nil, 0, err
	}
	if err := validateAlertLevel(f.AlertLevel); err != nil {
		return nil, 0, err
	}
	rows, total, err := s.repo.Detail(ctx, f, now)
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].LimitUSD = round2(rows[i].LimitUSD)
		rows[i].UsedUSD = round2(rows[i].UsedUSD)
		rows[i].UnusedUSD = round2(rows[i].UnusedUSD)
	}
	return rows, total, nil
}

// Snapshots 校验期末区间后透传趋势，并对每个桶的 _usd 金额做边界 round2（SubscriptionCount 计数不动）。
func (s *service) Snapshots(ctx context.Context, tenantID *int64, start, end int64) ([]SnapshotPoint, error) {
	if err := validateRange(start, end); err != nil {
		return nil, err
	}
	pts, err := s.repo.SnapshotTrend(ctx, tenantID, start, end)
	if err != nil {
		return nil, err
	}
	for i := range pts {
		pts[i].ExpiredUnusedUSD = round2(pts[i].ExpiredUnusedUSD)
		pts[i].ActiveRemainingUSD = round2(pts[i].ActiveRemainingUSD)
	}
	return pts, nil
}

// RunSnapshot 采集当下快照 → 幂等 Upsert → best-effort 告警。返回落库条数（供 job 记日志）。
//
// 流程：
//  1. Repo.CollectSnapshots 只读装配「当下应落库」的快照行（tenantID=nil 跨租户全量）。
//  2. Repo.UpsertSnapshots 幂等落库（唯一键去重，可重跑）。
//  3. maybeAlert 扫沉淀阈值触发告警——告警属旁路，失败不回滚快照、不改返回条数。
//
// 落库失败向上返回（job 记 error）；告警失败被吞（仅副作用，不影响 job 主流程与返回）。
func (s *service) RunSnapshot(ctx context.Context, tenantID *int64, now int64) (int, error) {
	rows, err := s.repo.CollectSnapshots(ctx, tenantID, now)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if err := s.repo.UpsertSnapshots(ctx, rows); err != nil {
		return 0, err
	}
	s.maybeAlert(ctx, rows, now)
	return len(rows), nil
}

// maybeAlert 是告警旁路：告警关闭 / 无 sink 时直接返回；否则扫已落库快照里「到期且未使用」的沉淀，
// 合计超阈则 Dispatch 一条到期沉淀告警（去重键按快照日，防同日轰炸）。best-effort：Dispatch 的
// error 被吞（仅记不到日志层面，本函数无返回；job 主流程不受影响）。
func (s *service) maybeAlert(ctx context.Context, rows []Snapshot, now int64) {
	if !s.cfg.Enabled || s.sink == nil {
		return
	}
	threshold := s.cfg.ThresholdPct
	if threshold <= 0 {
		threshold = alert.DefaultThresholdPct
	}

	var expiredUnused float64
	var expiredCount int
	for _, r := range rows {
		// 惰性过期口径：按 period_end < now 判到期，不单信 Status。
		if r.PeriodEnd < now && r.UnusedUSD > 0 {
			expiredUnused += r.UnusedUSD
			expiredCount++
		}
	}
	if expiredCount == 0 {
		return
	}

	// 沉淀率 = 到期未使用 / 到期额度上限；上限为 0 时保守视为 100%（有沉淀即触发）。
	var expiredLimit float64
	for _, r := range rows {
		if r.PeriodEnd < now && r.UnusedUSD > 0 {
			expiredLimit += r.LimitUSD
		}
	}
	pct := breakagePct(expiredUnused, expiredLimit)
	if pct < threshold {
		return
	}

	// 去重键按快照锚点日（now 的整天）：同一天同一沉淀告警只发一次，防重复 job 轰炸。
	day := now - now%86400
	_ = s.sink.Dispatch(ctx, alert.Alert{
		Level:    alert.LevelWarning,
		Subject:  "breakage 到期未使用额度沉淀告警",
		Body:     fmt.Sprintf("检测到 %d 笔到期未使用订阅，沉淀合计 %.2f USD，沉淀率 %.1f%%（阈值 %.1f%%）。", expiredCount, round2(expiredUnused), pct, threshold),
		DedupKey: fmt.Sprintf("breakage_expired_unused:%d", day),
	})
}

// ============================================================================
// 纯函数辅助（无副作用，便于单测）
// ============================================================================

// round2 半远零舍入到 2 位小数（金额边界统一口径，与仓库 QuotaRound 半远零一致但保 2 位）。
// 幂等：round2(round2(x)) == round2(x)，故与 handler 边界重复 round2 安全。
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// breakagePct 计算沉淀率（未使用/上限*100）：
//   - limit > 0  → unused/limit*100；
//   - limit <= 0 且 unused > 0 → 100（上限缺失但有沉淀，保守视为满，触发告警）；
//   - limit <= 0 且 unused <= 0 → 0（无沉淀无风险）。
//
// 避免除零产生 +Inf/NaN，保证告警判定用有限值。
func breakagePct(unused, limit float64) float64 {
	if limit > 0 {
		return unused / limit * 100
	}
	if unused > 0 {
		return 100
	}
	return 0
}

// validateRange 校验期末时间区间：任一端 <=0 视为不设该端（放行，交给 Repo 兜底）；
// 两端都设时要求 start <= end 且跨度 <= 366 天，否则 ErrRangeInvalid。
func validateRange(start, end int64) error {
	if start <= 0 || end <= 0 {
		return nil
	}
	if start > end || end-start > maxRangeSeconds {
		return ErrRangeInvalid
	}
	return nil
}

// validateAlertLevel 校验 Filter.AlertLevel 过滤值：空串（不过滤）与 warn/critical/exhausted 合法，
// 否则 ErrAlertLevelInvalid。AlertNone（空串）语义是「不过滤」，非「过滤健康行」。
func validateAlertLevel(l AlertLevel) error {
	switch l {
	case AlertNone, AlertWarn, AlertCritical, AlertExhausted:
		return nil
	default:
		return ErrAlertLevelInvalid
	}
}
