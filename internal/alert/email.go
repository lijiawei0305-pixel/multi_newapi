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
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Channel 是单个告警投递通道（邮件/webhook 各一个实现）。分发器（Sink）持有一组 Channel，
// best-effort 逐个投递：任一通道 Send 失败只记日志、不阻断其余通道、不打断主流程。
type Channel interface {
	// Name 通道名（日志用）。
	Name() string
	// Send 投递一条告警。cfg 为当次分发的运行期配置（含收件人/URL）。
	// 通道自行判断是否「可投」（如邮件无收件人 → 直接返回 nil 跳过，不算失败）。
	Send(ctx context.Context, cfg Config, a Alert) error
}

// EmailSender 是底层发信函数签名，默认绑定 common.SendEmail（复用仓库现有 SMTP 实现，不自造）。
// 抽成函数类型是为了单测注入假发信器——测试绝不真发邮件。
// 参数与 common.SendEmail 一致：subject 主题、receiver 收件人（多个用 ';' 分隔）、content HTML 正文。
type EmailSender func(subject, receiver, content string) error

// emailChannel 邮件通道：把 Alert 渲染成 HTML 正文，经 EmailSender 发给 Config.Email 列表。
type emailChannel struct {
	send EmailSender
}

// NewEmailChannel 构造邮件通道。send 为 nil 时回退 common.SendEmail（生产默认，复用现有 SMTP）。
func NewEmailChannel(send EmailSender) Channel {
	if send == nil {
		send = common.SendEmail
	}
	return &emailChannel{send: send}
}

// Name 通道名。
func (c *emailChannel) Name() string { return "email" }

// Send 把告警发到配置的收件人。无收件人视为「本通道未启用」→ 返回 nil 跳过（非失败）。
func (c *emailChannel) Send(_ context.Context, cfg Config, a Alert) error {
	receivers := make([]string, 0, len(cfg.Email))
	for _, addr := range cfg.Email {
		if addr = strings.TrimSpace(addr); addr != "" {
			receivers = append(receivers, addr)
		}
	}
	if len(receivers) == 0 {
		// 未配收件人：邮件通道不启用，静默跳过。
		return nil
	}
	// common.SendEmail 用 ';' 分隔多收件人。
	receiver := strings.Join(receivers, ";")
	subject := strings.TrimSpace(a.Subject)
	if subject == "" {
		subject = "系统告警"
	}
	return c.send(subject, receiver, renderEmailHTML(a))
}

// renderEmailHTML 把告警渲染成简单 HTML 正文（转义用户/系统文本，保留换行）。
func renderEmailHTML(a Alert) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<p><strong>级别：</strong>%s</p>", html.EscapeString(alertLevelZh(a.Level))))
	if subject := strings.TrimSpace(a.Subject); subject != "" {
		b.WriteString(fmt.Sprintf("<p><strong>主题：</strong>%s</p>", html.EscapeString(subject)))
	}
	body := strings.TrimSpace(a.Body)
	if body != "" {
		escaped := html.EscapeString(body)
		escaped = strings.ReplaceAll(escaped, "\n", "<br/>")
		b.WriteString(fmt.Sprintf("<p>%s</p>", escaped))
	}
	return b.String()
}

// alertLevelZh 把 Level 映射成中文展示名（W5：面向用户文案中文）。未知级别原样返回。
func alertLevelZh(l Level) string {
	switch l {
	case LevelInfo:
		return "信息"
	case LevelWarning:
		return "警告"
	case LevelCritical:
		return "严重"
	default:
		return string(l)
	}
}
