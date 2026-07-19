package ollama

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func openAIChatToOllamaChat(c *gin.Context, r *dto.GeneralOpenAIRequest) (*OllamaChatRequest, error) {
	chatReq := &OllamaChatRequest{
		Model:   r.Model,
		Stream:  r.Stream,
		Options: map[string]any{},
		Think:   r.Think,
	}
	if r.ResponseFormat != nil {
		if r.ResponseFormat.Type == "json" {
			chatReq.Format = "json"
		} else if r.ResponseFormat.Type == "json_schema" {
			if len(r.ResponseFormat.JsonSchema) > 0 {
				var schema any
				_ = common.Unmarshal(r.ResponseFormat.JsonSchema, &schema)
				chatReq.Format = schema
			}
		}
	}

	// options mapping
	if r.Temperature != nil {
		chatReq.Options["temperature"] = r.Temperature
	}
	if r.TopP != nil {
		chatReq.Options["top_p"] = lo.FromPtr(r.TopP)
	}
	if r.TopK != nil {
		chatReq.Options["top_k"] = lo.FromPtr(r.TopK)
	}
	if r.FrequencyPenalty != nil {
		chatReq.Options["frequency_penalty"] = lo.FromPtr(r.FrequencyPenalty)
	}
	if r.PresencePenalty != nil {
		chatReq.Options["presence_penalty"] = lo.FromPtr(r.PresencePenalty)
	}
	if r.Seed != nil {
		chatReq.Options["seed"] = int(lo.FromPtr(r.Seed))
	}
	if r.MaxCompletionTokens != nil || r.MaxTokens != nil {
		chatReq.Options["num_predict"] = common.SaturatingUintToInt(r.GetMaxTokens())
	}

	if r.Stop != nil {
		switch v := r.Stop.(type) {
		case string:
			chatReq.Options["stop"] = []string{v}
		case []string:
			chatReq.Options["stop"] = v
		case []any:
			arr := make([]string, 0, len(v))
			for _, i := range v {
				if s, ok := i.(string); ok {
					arr = append(arr, s)
				}
			}
			if len(arr) > 0 {
				chatReq.Options["stop"] = arr
			}
		}
	}

	if len(r.Tools) > 0 {
		tools := make([]OllamaTool, 0, len(r.Tools))
		for _, t := range r.Tools {
			tools = append(tools, OllamaTool{Type: "function", Function: OllamaToolFunction{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters}})
		}
		chatReq.Tools = tools
	}

	chatReq.Messages = make([]OllamaChatMessage, 0, len(r.Messages))
	for _, m := range r.Messages {
		var textBuilder strings.Builder
		var images []string
		if m.IsStringContent() {
			textBuilder.WriteString(m.StringContent())
		} else {
			parts := m.ParseContent()
			for _, part := range parts {
				if part.Type == dto.ContentTypeImageURL {
					source := part.ToFileSource()
					if source != nil {
						base64Data, _, err := service.GetBase64Data(c, source, "fetch image for ollama chat")
						if err != nil {
							return nil, err
						}
						if base64Data != "" {
							images = append(images, base64Data)
						}
					}
				} else if part.Type == dto.ContentTypeText {
					textBuilder.WriteString(part.Text)
				}
			}
		}
		cm := OllamaChatMessage{Role: m.Role, Content: textBuilder.String()}
		if len(images) > 0 {
			cm.Images = images
		}
		if m.Role == "tool" && m.Name != nil {
			cm.ToolName = *m.Name
		}
		if m.ToolCalls != nil && len(m.ToolCalls) > 0 {
			parsed := m.ParseToolCalls()
			if len(parsed) > 0 {
				calls := make([]OllamaToolCall, 0, len(parsed))
				for _, tc := range parsed {
					var args interface{}
					if tc.Function.Arguments != "" {
						_ = common.Unmarshal([]byte(tc.Function.Arguments), &args)
					}
					if args == nil {
						args = map[string]any{}
					}
					oc := OllamaToolCall{}
					oc.Function.Name = tc.Function.Name
					oc.Function.Arguments = args
					calls = append(calls, oc)
				}
				cm.ToolCalls = calls
			}
		}
		chatReq.Messages = append(chatReq.Messages, cm)
	}
	return chatReq, nil
}

// openAIToGenerate converts OpenAI completions request to Ollama generate
func openAIToGenerate(c *gin.Context, r *dto.GeneralOpenAIRequest) (*OllamaGenerateRequest, error) {
	gen := &OllamaGenerateRequest{
		Model:   r.Model,
		Stream:  r.Stream,
		Options: map[string]any{},
		Think:   r.Think,
	}
	// Prompt may be in r.Prompt (string or []any)
	if r.Prompt != nil {
		switch v := r.Prompt.(type) {
		case string:
			gen.Prompt = v
		case []any:
			var sb strings.Builder
			for _, it := range v {
				if s, ok := it.(string); ok {
					sb.WriteString(s)
				}
			}
			gen.Prompt = sb.String()
		default:
			gen.Prompt = fmt.Sprintf("%v", r.Prompt)
		}
	}
	if r.Suffix != nil {
		if s, ok := r.Suffix.(string); ok {
			gen.Suffix = s
		}
	}
	if r.ResponseFormat != nil {
		if r.ResponseFormat.Type == "json" {
			gen.Format = "json"
		} else if r.ResponseFormat.Type == "json_schema" {
			var schema any
			_ = common.Unmarshal(r.ResponseFormat.JsonSchema, &schema)
			gen.Format = schema
		}
	}
	if r.Temperature != nil {
		gen.Options["temperature"] = r.Temperature
	}
	if r.TopP != nil {
		gen.Options["top_p"] = lo.FromPtr(r.TopP)
	}
	if r.TopK != nil {
		gen.Options["top_k"] = lo.FromPtr(r.TopK)
	}
	if r.FrequencyPenalty != nil {
		gen.Options["frequency_penalty"] = lo.FromPtr(r.FrequencyPenalty)
	}
	if r.PresencePenalty != nil {
		gen.Options["presence_penalty"] = lo.FromPtr(r.PresencePenalty)
	}
	if r.Seed != nil {
		gen.Options["seed"] = int(lo.FromPtr(r.Seed))
	}
	if r.MaxCompletionTokens != nil || r.MaxTokens != nil {
		gen.Options["num_predict"] = common.SaturatingUintToInt(r.GetMaxTokens())
	}
	if r.Stop != nil {
		switch v := r.Stop.(type) {
		case string:
			gen.Options["stop"] = []string{v}
		case []string:
			gen.Options["stop"] = v
		case []any:
			arr := make([]string, 0, len(v))
			for _, i := range v {
				if s, ok := i.(string); ok {
					arr = append(arr, s)
				}
			}
			if len(arr) > 0 {
				gen.Options["stop"] = arr
			}
		}
	}
	return gen, nil
}

func requestOpenAI2Embeddings(r dto.EmbeddingRequest) *OllamaEmbeddingRequest {
	opts := map[string]any{}
	if r.Temperature != nil {
		opts["temperature"] = r.Temperature
	}
	if r.TopP != nil {
		opts["top_p"] = lo.FromPtr(r.TopP)
	}
	if r.FrequencyPenalty != nil {
		opts["frequency_penalty"] = lo.FromPtr(r.FrequencyPenalty)
	}
	if r.PresencePenalty != nil {
		opts["presence_penalty"] = lo.FromPtr(r.PresencePenalty)
	}
	if r.Seed != nil {
		opts["seed"] = int(lo.FromPtr(r.Seed))
	}
	dimensions := lo.FromPtrOr(r.Dimensions, 0)
	if r.Dimensions != nil {
		opts["dimensions"] = dimensions
	}
	input := r.ParseInput()
	if len(input) == 1 {
		return &OllamaEmbeddingRequest{Model: r.Model, Input: input[0], Options: opts, Dimensions: r.Dimensions}
	}
	return &OllamaEmbeddingRequest{Model: r.Model, Input: input, Options: opts, Dimensions: r.Dimensions}
}

func ollamaEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var oResp OllamaEmbeddingResponse
	defer service.CloseResponseBodyGracefully(resp)
	body, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err = common.Unmarshal(body, &oResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oResp.Error != "" {
		return nil, service.MarkExplicitUpstreamRejection(types.NewOpenAIError(fmt.Errorf("ollama error: %s", oResp.Error), types.ErrorCodeBadResponseBody, http.StatusBadGateway))
	}
	data := make([]dto.OpenAIEmbeddingResponseItem, 0, len(oResp.Embeddings))
	for i, emb := range oResp.Embeddings {
		data = append(data, dto.OpenAIEmbeddingResponseItem{Index: i, Object: "embedding", Embedding: emb})
	}
	usage := &dto.Usage{PromptTokens: oResp.PromptEvalCount, CompletionTokens: 0, TotalTokens: oResp.PromptEvalCount}
	embResp := &dto.OpenAIEmbeddingResponse{Object: "list", Data: data, Model: info.UpstreamModelName, Usage: *usage}
	out, _ := common.Marshal(embResp)
	service.IOCopyBytesGracefully(c, resp, out)
	return usage, nil
}

func FetchOllamaModels(baseURL, apiKey string) ([]OllamaModel, error) {
	return FetchOllamaModelsWithContext(context.Background(), baseURL, apiKey)
}

func FetchOllamaModelsWithContext(ctx context.Context, baseURL, apiKey string) ([]OllamaModel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/api/tags", baseURL)

	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// Ollama 通常不需要 Bearer token，但为了兼容性保留
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
		if readErr != nil {
			return nil, fmt.Errorf("读取错误响应失败: %w", readErr)
		}
		return nil, fmt.Errorf("服务器返回错误 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
	}

	var tagsResponse OllamaTagsResponse
	body, err := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	err = common.Unmarshal(body, &tagsResponse)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	return tagsResponse.Models, nil
}

// 拉取 Ollama 模型 (非流式)
func PullOllamaModel(baseURL, apiKey, modelName string) error {
	return PullOllamaModelWithContext(context.Background(), baseURL, apiKey, modelName)
}

func PullOllamaModelWithContext(ctx context.Context, baseURL, apiKey, modelName string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	url := fmt.Sprintf("%s/api/pull", baseURL)

	pullRequest := OllamaPullRequest{
		Name:   modelName,
		Stream: common.GetPointer(false), // 非流式，简化处理
	}

	requestBody, err := common.Marshal(pullRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client := &http.Client{
		Timeout: 30 * 60 * 1000 * time.Millisecond, // 30分钟超时，支持大模型
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
		if readErr != nil {
			return fmt.Errorf("读取错误响应失败: %w", readErr)
		}
		return fmt.Errorf("拉取模型失败 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
	}

	return nil
}

// 流式拉取 Ollama 模型 (支持进度回调)
func PullOllamaModelStream(baseURL, apiKey, modelName string, progressCallback func(OllamaPullResponse)) error {
	return PullOllamaModelStreamWithContext(context.Background(), baseURL, apiKey, modelName, progressCallback)
}

func PullOllamaModelStreamWithContext(ctx context.Context, baseURL, apiKey, modelName string, progressCallback func(OllamaPullResponse)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	url := fmt.Sprintf("%s/api/pull", baseURL)

	pullRequest := OllamaPullRequest{
		Name:   modelName,
		Stream: common.GetPointer(true), // 启用流式
	}

	requestBody, err := common.Marshal(pullRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client := &http.Client{
		Timeout: 60 * 60 * 1000 * time.Millisecond, // 1小时超时，支持超大模型
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
		if readErr != nil {
			return fmt.Errorf("读取错误响应失败: %w", readErr)
		}
		return fmt.Errorf("拉取模型失败 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
	}

	// 读取流式响应
	scanner := helper.NewStreamScanner(response.Body)
	successful := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var pullResponse OllamaPullResponse
		if err := common.Unmarshal([]byte(line), &pullResponse); err != nil {
			continue // 忽略解析失败的行
		}

		if progressCallback != nil {
			progressCallback(pullResponse)
		}

		// 检查是否出现错误或完成
		if strings.EqualFold(pullResponse.Status, "error") {
			return fmt.Errorf("拉取模型失败: %s", strings.TrimSpace(line))
		}
		if strings.EqualFold(pullResponse.Status, "success") {
			successful = true
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取流式响应失败: %v", err)
	}

	if !successful {
		return fmt.Errorf("拉取模型未完成: 未收到成功状态")
	}

	return nil
}

// 删除 Ollama 模型
func DeleteOllamaModel(baseURL, apiKey, modelName string) error {
	return DeleteOllamaModelWithContext(context.Background(), baseURL, apiKey, modelName)
}

func DeleteOllamaModelWithContext(ctx context.Context, baseURL, apiKey, modelName string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/api/delete", baseURL)

	deleteRequest := OllamaDeleteRequest{
		Name: modelName,
	}

	requestBody, err := common.Marshal(deleteRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
		if readErr != nil {
			return fmt.Errorf("读取错误响应失败: %w", readErr)
		}
		return fmt.Errorf("删除模型失败 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
	}

	return nil
}

func FetchOllamaVersion(baseURL, apiKey string) (string, error) {
	return FetchOllamaVersionWithContext(context.Background(), baseURL, apiKey)
}

func FetchOllamaVersionWithContext(ctx context.Context, baseURL, apiKey string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	trimmedBase := strings.TrimRight(baseURL, "/")
	if trimmedBase == "" {
		return "", fmt.Errorf("baseURL 为空")
	}

	url := fmt.Sprintf("%s/api/version", trimmedBase)

	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	body, err := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询版本失败 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
	}

	var versionResp struct {
		Version string `json:"version"`
	}

	if err := common.Unmarshal(body, &versionResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if versionResp.Version == "" {
		return "", fmt.Errorf("未返回版本信息")
	}

	return versionResp.Version, nil
}
