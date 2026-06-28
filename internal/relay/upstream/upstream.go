// Package upstream 是 relay.UpstreamPool 的真实适配器：把一次中继请求转发到 OpenAI 兼容
// 的上游网关（POST {BASE_URL}/chat/completions，Authorization: Bearer {API_KEY}），
// 原样回送上游响应体并解析 usage{prompt_tokens,completion_tokens} 作为计费基数。
//
// 上游凭据（BASE_URL / API_KEY）由 cmd/main 从环境变量注入，**绝不写入仓库/代码**。
// 本适配器只复用上游渠道、不感知租户/计费——选桶与扣费在 Billing 层完成（detailed-design §1.5）。
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/relay"
)

// 编译期断言：*Pool 满足 relay.UpstreamPool 契约（可直接注入 RelayGateway 或被 cmd handler 调用）。
var _ relay.UpstreamPool = (*Pool)(nil)

// defaultTimeout 是单次上游调用的超时上限（LLM 推理可能较慢，留足余量）。
const defaultTimeout = 120 * time.Second

// Pool 是单上游适配器（一期固定一个 OpenAI 兼容上游；多渠道/权重/熔断顺延后续 Slice）。
type Pool struct {
	baseURL string       // 形如 https://www.codexapis.com/v1（末尾 /chat/completions 由本包补全）
	apiKey  string       // 上游 Bearer 凭据（来自 env，禁止入库）
	client  *http.Client // 复用连接池
}

// New 用上游 base URL 与 API Key 构造转发池。baseURL 末尾的 "/" 会被规整。
func New(baseURL, apiKey string) *Pool {
	return &Pool{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		client:  &http.Client{Timeout: defaultTimeout},
	}
}

// upstreamUsage 对齐 OpenAI 兼容响应的 usage 段。
// 说明：OpenAI 口径下 completion_tokens 已**包含** reasoning_tokens（后者仅在
// completion_tokens_details 中再细分），故此处直接取 completion_tokens 即已计入推理 token，
// 不再二次累加以免重复计费（见任务说明「reasoning_tokens 计入 completion」）。
type upstreamUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// usageEnvelope 仅解析 usage 段；其余字段（choices 等）原样透传给客户端，不在本层解析。
type usageEnvelope struct {
	Usage upstreamUsage `json:"usage"`
}

// Forward 转发一次中继请求到上游 /chat/completions，返回上游响应原文 + 解析出的 usage。
//
//   - 传输层失败（连不上 / 读取失败）返回 error，由调用方标准化为 UPSTREAM_ERROR。
//   - 上游有响应（含 4xx/5xx）一律 error=nil 并在 RelayResponse 携带原始状态码/响应体，
//     由调用方决定「非 2xx 不计费、原样回送」。
func (p *Pool) Forward(ctx context.Context, req relay.RelayRequest) (relay.RelayResponse, relay.Usage, error) {
	url := p.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return relay.RelayResponse{}, relay.Usage{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return relay.RelayResponse{}, relay.Usage{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return relay.RelayResponse{}, relay.Usage{}, err
	}

	out := relay.RelayResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    map[string]string{"Content-Type": contentTypeOf(resp)},
	}
	return out, parseUsage(body), nil
}

// parseUsage 从上游响应体提取 usage；缺失/解析失败返回零用量（不阻断回送，仅计费基数为 0）。
func parseUsage(body []byte) relay.Usage {
	var env usageEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return relay.Usage{}
	}
	u := relay.Usage{
		PromptTokens:     env.Usage.PromptTokens,
		CompletionTokens: env.Usage.CompletionTokens,
		TotalTokens:      env.Usage.TotalTokens,
	}
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

// contentTypeOf 取上游 Content-Type，缺省回退 application/json。
func contentTypeOf(resp *http.Response) string {
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	return "application/json"
}
