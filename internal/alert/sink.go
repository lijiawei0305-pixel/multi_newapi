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
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// DefaultDedupTTL 是去重窗口的兜底时长：同一 DedupKey 在此窗口内只分发一次，防告警轰炸。
const DefaultDedupTTL = 30 * time.Minute

// ConfigProvider 返回当次分发要用的运行期配置。每次 Dispatch 都调它重读，
// 让管理员在「系统设置 → 告警」的改动即时生效（无需重启）。生产由 Core-C 装配层绑成
// func() Config { return LoadConfig(optionGetter) }。
type ConfigProvider func() Config

// logf 是日志钩子（默认 common.SysError），抽出便于单测断言/静默。
type logf func(format string, args ...any)

// Sink 是多通道告警分发器，实现 AlertSink。
//
// 语义（对齐 alert.go 契约）：
//   - Config.Enabled=false → Dispatch 直接 no-op（不读通道、不返错）。
//   - 同 DedupKey 在 TTL 窗口内只分发一次（DedupKey=="" 表示不去重，每次都发）。
//   - best-effort：逐通道投递，任一通道失败仅记日志、不阻断其余通道、不 panic；
//     Dispatch 返回的 error 仅为「最后一个失败通道」的错误，供调用方记日志参考，不代表主流程失败。
//
// 并发安全：去重表有独立锁；通道本身无状态。
type Sink struct {
	provider ConfigProvider
	channels []Channel
	dedupTTL time.Duration
	logFn    logf

	mu       sync.Mutex
	lastSent map[string]time.Time // DedupKey -> 上次分发时间
	// now 便于单测注入时钟；nil 时用 time.Now。
	now func() time.Time
}

// SinkOption 是 Sink 的可选配置。
type SinkOption func(*Sink)

// WithChannels 覆盖通道集合（默认 = 邮件 + webhook 生产通道）。单测用它注入假通道。
func WithChannels(channels ...Channel) SinkOption {
	return func(s *Sink) { s.channels = channels }
}

// WithDedupTTL 设置去重窗口时长；<=0 回退 DefaultDedupTTL。
func WithDedupTTL(ttl time.Duration) SinkOption {
	return func(s *Sink) {
		if ttl > 0 {
			s.dedupTTL = ttl
		}
	}
}

// WithLogger 覆盖日志钩子（单测静默或断言用）。
func WithLogger(l logf) SinkOption {
	return func(s *Sink) {
		if l != nil {
			s.logFn = l
		}
	}
}

// WithClock 注入时钟（单测控制去重窗口过期）。
func WithClock(now func() time.Time) SinkOption {
	return func(s *Sink) {
		if now != nil {
			s.now = now
		}
	}
}

// NewSink 构造分发器。provider 提供每次分发的配置（nil → 视为禁用，全程 no-op）。
// 不传 WithChannels 时默认装配生产的邮件 + webhook 通道。
func NewSink(provider ConfigProvider, opts ...SinkOption) *Sink {
	s := &Sink{
		provider: provider,
		dedupTTL: DefaultDedupTTL,
		logFn:    func(format string, args ...any) { common.SysError(fmt.Sprintf(format, args...)) },
		lastSent: make(map[string]time.Time),
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.channels == nil {
		s.channels = []Channel{
			NewEmailChannel(nil),
			NewWebhookChannel(nil, 0),
		}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// 编译期断言 Sink 实现 AlertSink。
var _ AlertSink = (*Sink)(nil)

// Dispatch best-effort 分发一条告警：禁用则 no-op；去重命中则跳过；否则逐通道投递，
// 通道失败只记日志不阻断。返回值仅供调用方参考（最后一个失败通道的错误），不打断主流程。
func (s *Sink) Dispatch(ctx context.Context, a Alert) error {
	if s == nil || s.provider == nil {
		return nil
	}
	cfg := s.provider()
	if !cfg.Enabled {
		// 总开关关闭：不发任何通道。
		return nil
	}

	// 去重：同 DedupKey 窗口内只发一次（空 key 不去重）。
	if a.DedupKey != "" && !s.markAndAllow(a.DedupKey) {
		return nil
	}

	var lastErr error
	for _, ch := range s.channels {
		if err := s.safeSend(ctx, ch, cfg, a); err != nil {
			lastErr = err
			s.logFn("[alert] 通道 %s 分发失败: %v", ch.Name(), err)
		}
	}
	return lastErr
}

// safeSend 调用单个通道的 Send 并捕获 panic，保证任一通道异常不打断分发循环/主流程。
func (s *Sink) safeSend(ctx context.Context, ch Channel, cfg Config, a Alert) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("通道 %s panic: %v", ch.Name(), r)
		}
	}()
	return ch.Send(ctx, cfg, a)
}

// markAndAllow 若该 key 不在去重窗口内（或已过期），记为已发并返回 true（放行）；
// 否则返回 false（窗口内，抑制）。顺带清理已过期条目，避免 map 无界增长。
func (s *Sink) markAndAllow(key string) bool {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.lastSent[key]; ok {
		if now.Sub(last) < s.dedupTTL {
			return false // 仍在窗口内，抑制。
		}
	}
	s.lastSent[key] = now
	s.pruneLocked(now)
	return true
}

// pruneLocked 清理已超出去重窗口的条目（调用方须持锁）。
func (s *Sink) pruneLocked(now time.Time) {
	for k, t := range s.lastSent {
		if now.Sub(t) >= s.dedupTTL {
			delete(s.lastSent, k)
		}
	}
}
