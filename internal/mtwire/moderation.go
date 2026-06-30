package mtwire

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/moderation"
	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/types"
)

// scanUserInputHook 是 agenthook.ScanUserInput 的实现：/v1 转发前扫描用户输入。
// 命中即记录违规（remind+block 都记，管理员可见）；block 级返回错误拦截，remind 级放行。
// best-effort：抽取/扫描/记录出错绝不阻断请求——仅在确切命中 block 级时返回错误。
func (a *App) scanUserInputHook(ctx context.Context, userID, tokenID int64, model string, request dto.Request) *types.NewAPIError {
	defer func() { _ = recover() }()
	if a.Moderator == nil {
		return nil
	}
	msgs := extractUserMessages(request)
	if len(msgs) == 0 {
		return nil
	}
	p := appctx.Principal{UserID: userID, TenantID: a.moderationTenantID(ctx, userID), Role: appctx.RoleUser}
	res, err := a.Moderator.ScanUserMessages(ctx, &p, msgs)
	if err != nil || res == nil || !res.Hit {
		return nil
	}
	if a.ModerationRepo != nil {
		_ = a.ModerationRepo.Record(ctx, &moderation.ViolationEvent{
			TenantID:     p.TenantID,
			UserID:       userID,
			TokenID:      tokenID,
			Model:        model,
			MatchedWords: res.Matches,
			Excerpt:      excerptOf(msgs),
			ActionTaken:  res.Action,
		})
	}
	if res.Action == moderation.ActionBlock {
		// 400（客户端错误）而非默认 500：违禁=用户输入问题，客户端（codex 等）不应重试，
		// 且能直接显示拦截提示文案，而非把 5xx 当临时错误反复重试。
		return types.NewErrorWithStatusCode(errors.New(res.Reminder), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest)
	}
	return nil // remind：放行（已记录）
}

// 注：租户解析用 a.moderationTenantID（站长按拥有的代理租户算，否则 users.tenant_id；见 agent.go）。
// TODO(perf): 热路径每请求查 tenant_id（站长再多查一次 owner_user_id）；后续按 userID 加短 TTL 缓存。

// extractUserMessages 从原生请求抽取「用户角色」的纯文本消息（满足「仅用户输入」）。
// 覆盖 chat completions(*dto.GeneralOpenAIRequest) 与 codex /responses(*dto.OpenAIResponsesRequest)；
// 其它格式（claude/gemini/embedding 等）暂不扫描（MVP，按需补）。
func extractUserMessages(request dto.Request) []moderation.Message {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return userMessagesFromChat(r.Messages)
	case *dto.OpenAIResponsesRequest:
		return userMessagesFromResponsesInput(r.Input)
	default:
		return nil
	}
}

func userMessagesFromChat(messages []dto.Message) []moderation.Message {
	// 只扫「最后一条用户消息」（本轮新输入）：多轮对话每次重发全部历史，历史消息已在各自轮次
	// 扫过；再整段扫会因旧消息里的违禁词反复误拦本轮无辜输入（如打"你好"却命中历史里的旧词）。
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		var out []moderation.Message
		for _, mc := range messages[i].ParseContent() {
			if mc.Type == dto.ContentTypeText && strings.TrimSpace(mc.Text) != "" {
				out = append(out, moderation.Message{Role: "user", Text: mc.Text})
			}
		}
		return out
	}
	return nil
}

// userMessagesFromResponsesInput 解析 /responses 的 input（json.RawMessage）：
// 形态一为纯字符串（即用户输入）；形态二为输入项数组，取 role=="user" 项的文本。
func userMessagesFromResponsesInput(input json.RawMessage) []moderation.Message {
	if len(input) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(input, &s) == nil {
		if strings.TrimSpace(s) != "" {
			return []moderation.Message{{Role: "user", Text: s}}
		}
		return nil
	}
	var items []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(input, &items) != nil {
		return nil
	}
	// 只扫最后一条 role=="user" 项（本轮新输入），不重扫历史（理由同 userMessagesFromChat）。
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "user" {
			continue
		}
		var out []moderation.Message
		for _, t := range responsesContentTexts(items[i].Content) {
			if strings.TrimSpace(t) != "" {
				out = append(out, moderation.Message{Role: "user", Text: t})
			}
		}
		return out
	}
	return nil
}

func responsesContentTexts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
	}
	return out
}

// excerptOf 取「最后一条非空用户消息」的截断片段，供管理员审阅上下文。
// 多数客户端（含 codex /responses）把真实用户 prompt 放在末尾，前面是系统/环境上下文，
// 故取末条比取首条更贴近实际违规文本（命中词本身在 MatchedWords 已脱敏）。
func excerptOf(msgs []moderation.Message) string {
	const maxLen = 120
	text := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.TrimSpace(msgs[i].Text) != "" {
			text = msgs[i].Text
			break
		}
	}
	runes := []rune(text)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "…"
	}
	return string(runes)
}
