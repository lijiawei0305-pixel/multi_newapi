package mtwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/moderation"
	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/types"
)

// scanUserInputHook 是 agenthook.ScanUserInput 的实现：/v1 转发前扫描用户输入。
// 命中即记录违规（remind+block 都记，管理员可见）；block 级返回错误拦截，remind 级放行。
// best-effort：抽取/扫描/记录出错绝不阻断请求——仅在确切命中 block 级时返回错误。
// 扫描失败与 panic 必须记日志（此前静默 recover 会让违禁词在故障时「隐形关闭」）。
func (a *App) scanUserInputHook(ctx context.Context, userID, tokenID int64, model string, request dto.Request) *types.NewAPIError {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: scanUserInputHook panic recovered: " + fmt.Sprint(r))
		}
	}()
	if a.Moderator == nil {
		return nil
	}
	msgs := extractUserMessages(request)
	if len(msgs) == 0 {
		return nil
	}
	p := appctx.Principal{UserID: userID, TenantID: a.moderationTenantID(ctx, userID), Role: appctx.RoleUser}
	res, err := a.Moderator.ScanUserMessages(ctx, &p, msgs)
	if err != nil {
		common.SysError("mtwire: moderation scan error (fail-open): " + err.Error())
		return nil
	}
	if res == nil || !res.Hit {
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

// extractUserMessages 从原生请求抽取「用户输入」纯文本，交违禁词扫描器。
//
// 安全原则（2026-07-16 修复越权绕过 · 见 RETRO「违禁词扫描格式/角色绕过」）：/v1 是无状态 HTTP，
// 服务端无从判断多轮历史是否真经过本平台；请求体内**一切客户端提供的文本**都会原样送往上游，
// 故一律视作「用户输入」全部抽出——不分端点格式、不分消息 role（system/assistant/tool 同为客户端
// 可控字节）、不分位置（不再只取末条）。全部标记为 moderation.Message{Role:"user"}：扫描器
// scannable() 仅放行 user/空 role，本层负责把入站文本统一归类为「可扫描的用户输入」（在抽取层重标，
// 而非放宽扫描器 role 契约——后者由 service_test.go:TestScan_OnlyUserRole 固化，不动）。
//
// 覆盖**全部**会到达本 hook 的 dto.Request 具体类型（见 relay/helper/valid_request.go 的
// GetAndValidateRequest：OpenAI/Responses/Compaction/Claude/Gemini(chat+embed+batchEmbed)/Image/
// Audio/Embedding/Rerank）；未识别类型（如 realtime 的空 *dto.BaseRequest）无文本可扫，返 nil。
func extractUserMessages(request dto.Request) []moderation.Message {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		// 同一类型覆盖 chat/completions/edits/moderations/FIM：messages 之外，
		// prompt/input/instruction/prefix/suffix 也都可能承载用户输入。
		out := textsToUserMessages(anyTexts(r.Prompt)...)
		out = append(out, textsToUserMessages(anyTexts(r.Input)...)...)
		out = append(out, textsToUserMessages(anyTexts(r.Prefix)...)...)
		out = append(out, textsToUserMessages(anyTexts(r.Suffix)...)...)
		out = append(out, textsToUserMessages(r.Instruction)...)
		out = append(out, openAIMessageTexts(r.Messages)...)
		return out
	case *dto.OpenAIResponsesRequest:
		return responsesInputTexts(r.Input)
	case *dto.OpenAIResponsesCompactionRequest:
		out := responsesInputTexts(r.Input)
		out = append(out, rawTexts(r.Instructions)...)
		return out
	case *dto.ClaudeRequest:
		return claudeTexts(r)
	case *dto.GeminiChatRequest:
		return geminiChatTexts(r)
	case *dto.GeminiEmbeddingRequest:
		return geminiContentTexts(&r.Content)
	case *dto.GeminiBatchEmbeddingRequest:
		var out []moderation.Message
		for _, req := range r.Requests {
			if req != nil {
				out = append(out, geminiContentTexts(&req.Content)...)
			}
		}
		return out
	case *dto.ImageRequest:
		return textsToUserMessages(r.Prompt)
	case *dto.AudioRequest:
		return textsToUserMessages(r.Input, r.Instructions)
	case *dto.EmbeddingRequest:
		return textsToUserMessages(r.ParseInput()...)
	case *dto.RerankRequest:
		return rerankTexts(r)
	default:
		return nil
	}
}

// textsToUserMessages 把若干纯文本片段包成「用户输入」消息（空白片段丢弃）。
func textsToUserMessages(texts ...string) []moderation.Message {
	out := make([]moderation.Message, 0, len(texts))
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			out = append(out, moderation.Message{Role: "user", Text: t})
		}
	}
	return out
}

// anyTexts 抽取 OpenAI 请求里 any 型字段（prompt/input/prefix/suffix）的文本：支持 string 与字符串数组。
func anyTexts(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// rawTexts 抽取 json.RawMessage 里的文本：形态一为 JSON 字符串，形态二为字符串数组，其余忽略。
func rawTexts(raw json.RawMessage) []moderation.Message {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if common.Unmarshal(raw, &s) == nil {
		return textsToUserMessages(s)
	}
	var arr []string
	if common.Unmarshal(raw, &arr) == nil {
		return textsToUserMessages(arr...)
	}
	return nil
}

// openAIMessageTexts 抽取 OpenAI chat messages 全部文本：不分 role（system/user/assistant/tool 皆客户端
// 可控）、不分位置（历史每轮重发，无状态服务端无从判断是否已扫过，故全扫），统一标记为 user 输入。
func openAIMessageTexts(messages []dto.Message) []moderation.Message {
	var out []moderation.Message
	for i := range messages {
		for _, mc := range messages[i].ParseContent() {
			if mc.Type == dto.ContentTypeText && strings.TrimSpace(mc.Text) != "" {
				out = append(out, moderation.Message{Role: "user", Text: mc.Text})
			}
		}
	}
	return out
}

// responsesInputTexts 解析 /responses 的 input（json.RawMessage）：形态一纯字符串；形态二输入项数组
// ——不分 role、不分位置全部抽取（理由同 openAIMessageTexts）。
func responsesInputTexts(input json.RawMessage) []moderation.Message {
	if len(input) == 0 {
		return nil
	}
	var s string
	if common.Unmarshal(input, &s) == nil {
		return textsToUserMessages(s)
	}
	var items []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if common.Unmarshal(input, &items) != nil {
		return nil
	}
	var out []moderation.Message
	for _, it := range items {
		out = append(out, textsToUserMessages(responsesContentTexts(it.Content)...)...)
	}
	return out
}

// claudeTexts 抽取 Claude 请求文本：system（字符串或分块）+ 遗留 prompt + 全部 messages（不分 role/位置）。
func claudeTexts(r *dto.ClaudeRequest) []moderation.Message {
	var out []moderation.Message
	if r.System != nil {
		if r.IsStringSystem() {
			out = append(out, textsToUserMessages(r.GetStringSystem())...)
		} else {
			for _, media := range r.ParseSystem() {
				if media.Type == "text" {
					out = append(out, textsToUserMessages(media.GetText())...)
				}
			}
		}
	}
	out = append(out, textsToUserMessages(r.Prompt)...)
	for i := range r.Messages {
		// ClaudeMessage.GetStringContent 同时处理字符串内容与分块内容（拼接 text 块）。
		out = append(out, textsToUserMessages(r.Messages[i].GetStringContent())...)
	}
	return out
}

// geminiChatTexts 抽取 Gemini 请求文本：systemInstruction + 全部 contents（不分 role/位置）+ 批量子请求。
func geminiChatTexts(r *dto.GeminiChatRequest) []moderation.Message {
	var out []moderation.Message
	out = append(out, geminiContentTexts(r.SystemInstructions)...)
	for i := range r.Contents {
		out = append(out, geminiContentTexts(&r.Contents[i])...)
	}
	for i := range r.Requests {
		out = append(out, geminiChatTexts(&r.Requests[i])...)
	}
	return out
}

// geminiContentTexts 抽取单个 Gemini content 的 parts 文本（nil content 返 nil）。
func geminiContentTexts(c *dto.GeminiChatContent) []moderation.Message {
	if c == nil {
		return nil
	}
	var out []moderation.Message
	for _, part := range c.Parts {
		out = append(out, textsToUserMessages(part.Text)...)
	}
	return out
}

// rerankTexts 抽取 rerank 请求文本：query + documents（[]any，元素可能是字符串或 {text:...} 对象）。
func rerankTexts(r *dto.RerankRequest) []moderation.Message {
	out := textsToUserMessages(r.Query)
	for _, doc := range r.Documents {
		switch d := doc.(type) {
		case string:
			out = append(out, textsToUserMessages(d)...)
		case map[string]any:
			if s, ok := d["text"].(string); ok {
				out = append(out, textsToUserMessages(s)...)
			}
		}
	}
	return out
}

func responsesContentTexts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if common.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if common.Unmarshal(raw, &parts) != nil {
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
