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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// defaultWebhookTimeout 是 webhook POST 的兜底超时（调用方 ctx 无 deadline 时生效）。
const defaultWebhookTimeout = 10 * time.Second

// webhookPayload 是 POST 给 webhook 的 JSON 结构（snake_case，通用可被飞书/钉钉/自建服务消费）。
type webhookPayload struct {
	Level    string `json:"level"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	DedupKey string `json:"dedup_key,omitempty"`
}

// webhookChannel webhook 通道：把 Alert 序列化成 JSON POST 到 Config.WebhookURL，带超时、best-effort。
type webhookChannel struct {
	client  *http.Client
	timeout time.Duration
}

// NewWebhookChannel 构造 webhook 通道。client 为 nil 时用带 timeout 的默认 http.Client；
// timeout <=0 时回退 defaultWebhookTimeout。
func NewWebhookChannel(client *http.Client, timeout time.Duration) Channel {
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &webhookChannel{client: client, timeout: timeout}
}

// Name 通道名。
func (c *webhookChannel) Name() string { return "webhook" }

// Send 把告警 POST 到 webhook URL。未配 URL 视为「本通道未启用」→ 返回 nil 跳过（非失败）。
func (c *webhookChannel) Send(ctx context.Context, cfg Config, a Alert) error {
	url := strings.TrimSpace(cfg.WebhookURL)
	if url == "" {
		// 未配 webhook：通道不启用，静默跳过。
		return nil
	}

	payload, err := common.Marshal(webhookPayload{
		Level:    string(a.Level),
		Subject:  a.Subject,
		Body:     a.Body,
		DedupKey: a.DedupKey,
	})
	if err != nil {
		return fmt.Errorf("webhook 序列化失败: %w", err)
	}

	// 保证有超时上限：调用方 ctx 无 deadline 时叠加本通道 timeout。
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook 构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook 请求失败: %w", err)
	}
	defer func() {
		// 读尽并关闭，便于连接复用。
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回非 2xx 状态: %d", resp.StatusCode)
	}
	return nil
}
