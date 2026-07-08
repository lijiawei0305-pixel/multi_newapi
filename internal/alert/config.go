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

package alert

import (
	"strconv"
	"strings"
)

// LoadConfig 实现 alert.go 冻结的 ConfigLoader 契约：从注入的 OptionGetter 读 4 个告警 option 键，
// 解析成运行期 Config。本文件不 import 任何业务/setting 包——getter 由装配层（Core-C wire）用
// DB option 的读函数适配后注入，保持 alert 包零外部依赖、可独立编译。
//
// 契约（与 alert.go ConfigLoader 文档逐条对齐）：
//   - Enabled     = get(OptEnabled) == "true"
//   - Email       = split(get(OptEmail))，逗号或换行分隔，去空白、丢空项
//   - WebhookURL  = trim(get(OptWebhookURL))
//   - ThresholdPct = parseFloat(get(OptThresholdPct))，非法或 <=0 → DefaultThresholdPct
//   - getter == nil → 返回全零值 Config（Enabled=false，「未配置即不告警」的安全默认）
//
// 编译期断言 LoadConfig 满足 ConfigLoader 签名，签名一旦漂移即编译失败。
var _ ConfigLoader = LoadConfig

// LoadConfig 按契约从 option 读并解析告警配置。
func LoadConfig(getter OptionGetter) Config {
	if getter == nil {
		// 未注入读函数：视为未配置，安全默认（禁用，不告警）。
		return Config{ThresholdPct: DefaultThresholdPct}
	}

	cfg := Config{
		Enabled:      strings.TrimSpace(getter(OptEnabled)) == "true",
		Email:        parseRecipients(getter(OptEmail)),
		WebhookURL:   strings.TrimSpace(getter(OptWebhookURL)),
		ThresholdPct: parseThreshold(getter(OptThresholdPct)),
	}
	return cfg
}

// parseRecipients 把逗号/换行/分号分隔的收件人串拆成去空白、去空项的切片。
// 返回 nil（而非空切片）当没有任何有效地址，语义等同「不走邮件通道」。
func parseRecipients(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if addr := strings.TrimSpace(f); addr != "" {
			out = append(out, addr)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseThreshold 解析阈值百分比；非法或 <=0 回退 DefaultThresholdPct。
func parseThreshold(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return DefaultThresholdPct
	}
	return v
}
