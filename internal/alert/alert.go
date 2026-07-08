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

// Package alert 是「可配置告警」的领域契约层（P2-BRK-01 一并交付）：定义 AlertSink 分发接口、
// Alert 事件、可配阈值 Config + option 键常量。只定义类型/接口/签名，实现落 internal/alert/*.go
// （email.go / webhook.go / 分发器）与 Core-C 的装配。
//
// 复用范围：breakage 异常（系统卡单/沉淀超阈）与满额订阅告警（收编 STATUS §三 7c-2「满额服务端
// 主动推送」可选待办）共用同一个 AlertSink——一次实现收两个待办。
//
// 配置来源：Config 从 DB option 读（与「系统设置 → 支付」凭据同一存储层，Builder A 管理的 setting
// 包 option）。本包不 import setting 包，改为定义 option 键常量 + LoadConfig(getter) 签名——由
// Core-C 在装配层把 setting 的读函数注入进来，保持本包零外部依赖、可独立编译。前端「系统设置 →
// 告警」表单与 Core-C 写读都用本文件的 Opt* 常量，键名单一来源不漂移。
//
// 分发语义（实现约定）：Dispatch best-effort（多通道并发/串行 best-effort，任一通道失败不阻断其余，
// 不影响主流程）+ 去重（同 DedupKey 在窗口内只发一次，防告警轰炸）。
package alert

import "context"

// ============================================================================
// 告警级别与事件
// ============================================================================

// Level 是告警事件的严重级别（用于通道选择/文案/去重维度）。
type Level string

const (
	// LevelInfo 信息级（如满额提示）。
	LevelInfo Level = "info"
	// LevelWarning 警告级（如 breakage 沉淀超阈、订阅接近满额）。
	LevelWarning Level = "warning"
	// LevelCritical 严重级（如系统异常卡单堆积）。
	LevelCritical Level = "critical"
)

// Alert 是一条待分发的告警事件。
type Alert struct {
	// Level 严重级别。
	Level Level
	// Subject 主题（邮件标题 / webhook title；简短单行，中文）。
	Subject string
	// Body 正文（邮件正文 / webhook message；可多行，中文）。
	Body string
	// DedupKey 去重键：同一 key 在去重窗口内只分发一次（如
	// "breakage_anomaly:20260708" 或 "sub_exhausted:<sub_id>:<period_end>"）。
	// 空串表示不去重（每次都发）。
	DedupKey string
}

// ============================================================================
// 分发接口
// ============================================================================

// AlertSink 是告警分发口。Dispatch best-effort：多通道分发，任一通道失败不阻断其余、不返回致命错误
// 打断主流程（返回的 error 仅供调用方记日志）；同 DedupKey 在窗口内只发一次。
//
// 调用方：breakage 快照 job（StartBreakageSnapshotLoop）扫到系统异常/满额订阅时 Dispatch；
// 满额订阅推送（收编 7c-2）同样经此口。
type AlertSink interface {
	Dispatch(ctx context.Context, a Alert) error
}

// ============================================================================
// 配置（从 DB option 读；键常量单一来源，前端设置页与 Core-C 共用）
// ============================================================================

// Config 是告警的运行期配置（从 DB option 读，管理员在「系统设置 → 告警」表单里配）。
type Config struct {
	// Enabled 总开关：false 时 AlertSink.Dispatch 直接放弃（不发任何通道）。
	Enabled bool
	// Email 邮件收件人列表（空 = 不走邮件通道）。逗号/换行分隔的 option 值由 LoadConfig 解析成切片。
	Email []string
	// WebhookURL webhook 目标 URL（空 = 不走 webhook 通道）。分发器 POST JSON。
	WebhookURL string
	// ThresholdPct 告警阈值（用量百分比，0..100）：订阅用量 >= 此值触发满额告警；
	// breakage 沉淀相关阈值同理复用。<=0 时实现侧回退默认（DefaultThresholdPct）。
	ThresholdPct float64
}

// DefaultThresholdPct 是阈值缺省/非法时的兜底（95%，对齐订阅监控 critical 档）。
const DefaultThresholdPct = 95.0

// ============================================================================
// Option 键常量（前端「系统设置 → 告警」表单 name 与 Core-C LoadConfig 读取键，单一来源）
// ============================================================================

const (
	// OptEnabled 告警总开关（"true"/"false"）。
	OptEnabled = "breakage_alert_enabled"
	// OptEmail 邮件收件人（逗号或换行分隔的多个地址）。
	OptEmail = "breakage_alert_email"
	// OptWebhookURL webhook 目标 URL。
	OptWebhookURL = "breakage_alert_webhook_url"
	// OptThresholdPct 告警阈值百分比（字符串数字，如 "95"）。
	OptThresholdPct = "breakage_alert_threshold_pct"
)

// OptionGetter 按 option 键返回其字符串值（缺失返回空串）。由 Core-C 在装配层用 setting/DB option
// 的读函数适配注入——本包不 import setting，保持零外部依赖可独立编译。
type OptionGetter func(key string) string

// ConfigLoader 是「从 option 读并解析告警配置」的函数契约（实现留给 Core-C，落 internal/alert 另一
// 文件，如 config.go）。约定契约：
//   - Enabled = get(OptEnabled) == "true"
//   - Email   = split(get(OptEmail)) 去空白/去空项（逗号或换行分隔）
//   - WebhookURL = trim(get(OptWebhookURL))
//   - ThresholdPct = parseFloat(get(OptThresholdPct))，非法/<=0 → DefaultThresholdPct
//   - getter 为 nil → 返回全零值 Config（Enabled=false，「未配置即不告警」的安全默认）。
//
// Core-C 落地时提供 func LoadConfig(getter OptionGetter) Config，签名与本类型一致——本文件只冻结
// 契约、不含实现，保证契约包可独立编译。
type ConfigLoader func(getter OptionGetter) Config
