package moderation

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// service 是 Moderator 的实现。
type service struct {
	repo    BannedWordRepo
	matcher Matcher
}

// NewService 构造内容审核服务。repo 提供词库，matcher 执行匹配。
func NewService(repo BannedWordRepo, m Matcher) Moderator {
	return &service{repo: repo, matcher: m}
}

func (s *service) ScanUserMessages(ctx context.Context, p *appctx.Principal, msgs []Message) (*ModerationResult, error) {
	words, err := s.effectiveWords(ctx, p)
	if err != nil {
		return nil, err
	}
	if len(words) == 0 {
		return &ModerationResult{}, nil
	}

	var matched []BannedWord
	seen := make(map[int64]bool)
	for _, msg := range msgs {
		if !scannable(msg.Role) || strings.TrimSpace(msg.Text) == "" {
			continue
		}
		for _, w := range s.matcher.Match(msg.Text, words) {
			if !seen[w.ID] {
				matched = append(matched, w)
				seen[w.ID] = true
			}
		}
	}
	if len(matched) == 0 {
		return &ModerationResult{}, nil
	}

	action := ActionRemind
	for _, w := range matched {
		if w.Action == ActionBlock {
			action = ActionBlock
			break
		}
	}
	return &ModerationResult{
		Hit:      true,
		Matches:  desensitizeAll(matched),
		Action:   action,
		Reminder: reminderText(action),
	}, nil
}

// effectiveWords 合并「全站基础库(tenant_id=0) ∪ 当前租户自有词」，仅保留启用的。
func (s *service) effectiveWords(ctx context.Context, p *appctx.Principal) ([]BannedWord, error) {
	base, err := s.repo.ListWords(ctx, 0)
	if err != nil {
		return nil, err
	}
	var tenant []BannedWord
	if p != nil && p.TenantID != 0 {
		if tenant, err = s.repo.ListWords(ctx, p.TenantID); err != nil {
			return nil, err
		}
	}
	all := make([]BannedWord, 0, len(base)+len(tenant))
	for _, w := range base {
		if w.Enabled {
			all = append(all, w)
		}
	}
	for _, w := range tenant {
		if w.Enabled {
			all = append(all, w)
		}
	}
	return all, nil
}

// scannable 判定某角色的消息是否纳入扫描——仅用户输入（"user" 或未标注角色）。
func scannable(role string) bool {
	return role == "" || role == "user"
}

// desensitize 脱敏：保留首字符，其余以 * 遮蔽（单字符词整体遮蔽）。
func desensitize(word string) string {
	runes := []rune(word)
	if len(runes) <= 1 {
		return "*"
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1)
}

func desensitizeAll(ws []BannedWord) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, desensitize(w.Word))
	}
	return out
}

// reminderText 返回面向用户的提醒文案（后续可做成可配置项）。
func reminderText(action ModerationAction) string {
	if action == ActionBlock {
		return "您的消息包含违禁内容，已被拦截，请修改后重试。"
	}
	return "温馨提示：您的消息可能包含敏感内容，请注意合规用语。"
}

var _ Moderator = (*service)(nil)
