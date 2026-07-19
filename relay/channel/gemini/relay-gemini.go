package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const thoughtSignatureBypassValue = "context_engineering_is_the_way_to_go"

// Setting safety to the lowest possible values since Gemini is already powerless enough
func CovertOpenAI2Gemini(c *gin.Context, textRequest dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) (*dto.GeminiChatRequest, error) {

	geminiRequest := dto.GeminiChatRequest{
		Contents: make([]dto.GeminiChatContent, 0, len(textRequest.Messages)),
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: textRequest.Temperature,
		},
	}

	if textRequest.TopP != nil {
		geminiRequest.GenerationConfig.TopP = common.GetPointer(*textRequest.TopP)
	}

	if textRequest.MaxCompletionTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = common.GetPointer(*textRequest.MaxCompletionTokens)
	} else if textRequest.MaxTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = common.GetPointer(*textRequest.MaxTokens)
	}

	if textRequest.Seed != nil {
		geminiSeed := int64(*textRequest.Seed)
		geminiRequest.GenerationConfig.Seed = common.GetPointer(geminiSeed)
	}

	attachThoughtSignature := (info.ChannelType == constant.ChannelTypeGemini ||
		info.ChannelType == constant.ChannelTypeVertexAi) &&
		model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled

	if model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{
			"TEXT",
			"IMAGE",
		}
	}
	if stopSequences := parseStopSequences(textRequest.Stop); len(stopSequences) > 0 {
		// Gemini supports up to 5 stop sequences
		if len(stopSequences) > 5 {
			stopSequences = stopSequences[:5]
		}
		geminiRequest.GenerationConfig.StopSequences = stopSequences
	}

	adaptorWithExtraBody := false

	// patch extra_body
	if len(textRequest.ExtraBody) > 0 {
		var extraBody map[string]interface{}
		if err := common.Unmarshal(textRequest.ExtraBody, &extraBody); err != nil {
			return nil, fmt.Errorf("invalid extra body: %w", err)
		}

		// eg. {"google":{"thinking_config":{"thinking_budget":5324,"include_thoughts":true}}}
		if googleBody, ok := extraBody["google"].(map[string]interface{}); ok {
			if !strings.HasSuffix(info.UpstreamModelName, "-nothinking") {
				adaptorWithExtraBody = true
				// check error param name like thinkingConfig, should be thinking_config
				if _, hasErrorParam := googleBody["thinkingConfig"]; hasErrorParam {
					return nil, errors.New("extra_body.google.thinkingConfig is not supported, use extra_body.google.thinking_config instead")
				}

				if thinkingConfig, ok := googleBody["thinking_config"].(map[string]interface{}); ok {
					// check error param name like thinkingBudget, should be thinking_budget
					if _, hasErrorParam := thinkingConfig["thinkingBudget"]; hasErrorParam {
						return nil, errors.New("extra_body.google.thinking_config.thinkingBudget is not supported, use extra_body.google.thinking_config.thinking_budget instead")
					}
					var hasThinkingConfig bool
					var tempThinkingConfig dto.GeminiThinkingConfig

					if thinkingBudget, exists := thinkingConfig["thinking_budget"]; exists {
						switch v := thinkingBudget.(type) {
						case float64:
							budgetInt := int(v)
							tempThinkingConfig.ThinkingBudget = common.GetPointer(budgetInt)
							if budgetInt > 0 {
								// 有正数预算
								tempThinkingConfig.IncludeThoughts = common.GetPointer(true)
							} else {
								// 存在但为0或负数，禁用思考
								tempThinkingConfig.IncludeThoughts = common.GetPointer(false)
							}
							hasThinkingConfig = true
						default:
							return nil, errors.New("extra_body.google.thinking_config.thinking_budget must be an integer")
						}
					}

					if includeThoughts, exists := thinkingConfig["include_thoughts"]; exists {
						if v, ok := includeThoughts.(bool); ok {
							tempThinkingConfig.IncludeThoughts = common.GetPointer(v)
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.include_thoughts must be a boolean")
						}
					}
					if thinkingLevel, exists := thinkingConfig["thinking_level"]; exists {
						if v, ok := thinkingLevel.(string); ok {
							tempThinkingConfig.ThinkingLevel = v
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.thinking_level must be a string")
						}
					}

					if hasThinkingConfig {
						// 避免 panic: 仅在获得配置时分配，防止后续赋值时空指针
						if geminiRequest.GenerationConfig.ThinkingConfig == nil {
							geminiRequest.GenerationConfig.ThinkingConfig = &tempThinkingConfig
						} else {
							// 如果已分配，则合并内容
							if tempThinkingConfig.ThinkingBudget != nil {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = tempThinkingConfig.ThinkingBudget
							}
							if tempThinkingConfig.IncludeThoughts != nil {
								geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts = tempThinkingConfig.IncludeThoughts
							}
							if tempThinkingConfig.ThinkingLevel != "" {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel = tempThinkingConfig.ThinkingLevel
							}
						}
					}
				}
			}

			// check error param name like imageConfig, should be image_config
			if _, hasErrorParam := googleBody["imageConfig"]; hasErrorParam {
				return nil, errors.New("extra_body.google.imageConfig is not supported, use extra_body.google.image_config instead")
			}

			if imageConfig, ok := googleBody["image_config"].(map[string]interface{}); ok {
				// check error param name like aspectRatio, should be aspect_ratio
				if _, hasErrorParam := imageConfig["aspectRatio"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.aspectRatio is not supported, use extra_body.google.image_config.aspect_ratio instead")
				}
				// check error param name like imageSize, should be image_size
				if _, hasErrorParam := imageConfig["imageSize"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.imageSize is not supported, use extra_body.google.image_config.image_size instead")
				}

				// convert snake_case to camelCase for Gemini API
				geminiImageConfig := make(map[string]interface{})
				if aspectRatio, ok := imageConfig["aspect_ratio"]; ok {
					geminiImageConfig["aspectRatio"] = aspectRatio
				}
				if imageSize, ok := imageConfig["image_size"]; ok {
					geminiImageConfig["imageSize"] = imageSize
				}

				if len(geminiImageConfig) > 0 {
					imageConfigBytes, err := common.Marshal(geminiImageConfig)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal image_config: %w", err)
					}
					geminiRequest.GenerationConfig.ImageConfig = imageConfigBytes
				}
			}
		}
	}

	if !adaptorWithExtraBody {
		ThinkingAdaptor(&geminiRequest, info, textRequest)
	}

	safetySettings := make([]dto.GeminiChatSafetySettings, 0, len(SafetySettingList))
	for _, category := range SafetySettingList {
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: model_setting.GetGeminiSafetySetting(category),
		})
	}
	geminiRequest.SafetySettings = safetySettings

	// openaiContent.FuncToToolCalls()
	if textRequest.Tools != nil {
		functions := make([]dto.FunctionRequest, 0, len(textRequest.Tools))
		googleSearch := false
		codeExecution := false
		urlContext := false
		for _, tool := range textRequest.Tools {
			if tool.Function.Name == "googleSearch" {
				googleSearch = true
				continue
			}
			if tool.Function.Name == "codeExecution" {
				codeExecution = true
				continue
			}
			if tool.Function.Name == "urlContext" {
				urlContext = true
				continue
			}
			if tool.Function.Parameters != nil {

				params, ok := tool.Function.Parameters.(map[string]interface{})
				if ok {
					if props, hasProps := params["properties"].(map[string]interface{}); hasProps {
						if len(props) == 0 {
							tool.Function.Parameters = nil
						}
					}
				}
			}
			// Clean the parameters before appending
			cleanedParams := cleanFunctionParameters(tool.Function.Parameters)
			tool.Function.Parameters = cleanedParams
			functions = append(functions, tool.Function)
		}
		geminiTools := geminiRequest.GetTools()
		if codeExecution {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				CodeExecution: make(map[string]string),
			})
		}
		if googleSearch {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				GoogleSearch: make(map[string]string),
			})
		}
		if urlContext {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				URLContext: make(map[string]string),
			})
		}
		if len(functions) > 0 {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				FunctionDeclarations: functions,
			})
		}
		geminiRequest.SetTools(geminiTools)

		// [NEW] Convert OpenAI tool_choice to Gemini toolConfig.functionCallingConfig
		// Mapping: "auto" -> "AUTO", "none" -> "NONE", "required" -> "ANY"
		// Object format: {"type": "function", "function": {"name": "xxx"}} -> "ANY" + allowedFunctionNames
		if textRequest.ToolChoice != nil {
			geminiRequest.ToolConfig = convertToolChoiceToGeminiConfig(textRequest.ToolChoice)
		}
	}

	if textRequest.ResponseFormat != nil && (textRequest.ResponseFormat.Type == "json_schema" || textRequest.ResponseFormat.Type == "json_object") {
		geminiRequest.GenerationConfig.ResponseMimeType = "application/json"

		if len(textRequest.ResponseFormat.JsonSchema) > 0 {
			// 先将json.RawMessage解析
			var jsonSchema dto.FormatJsonSchema
			if err := common.Unmarshal(textRequest.ResponseFormat.JsonSchema, &jsonSchema); err == nil {
				cleanedSchema := removeAdditionalPropertiesWithDepth(jsonSchema.Schema, 0)
				geminiRequest.GenerationConfig.ResponseSchema = cleanedSchema
			}
		}
	}
	tool_call_ids := make(map[string]string)
	var system_content []string
	//shouldAddDummyModelMessage := false
	for _, message := range textRequest.Messages {
		if message.Role == "system" || message.Role == "developer" {
			system_content = append(system_content, message.StringContent())
			continue
		} else if message.Role == "tool" || message.Role == "function" {
			if len(geminiRequest.Contents) == 0 || geminiRequest.Contents[len(geminiRequest.Contents)-1].Role == "model" {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role: "user",
				})
			}
			var parts = &geminiRequest.Contents[len(geminiRequest.Contents)-1].Parts
			name := ""
			if message.Name != nil {
				name = *message.Name
			} else if val, exists := tool_call_ids[message.ToolCallId]; exists {
				name = val
			}
			var contentMap map[string]interface{}
			contentStr := message.StringContent()

			// 1. 尝试解析为 JSON 对象
			if err := common.Unmarshal([]byte(contentStr), &contentMap); err != nil {
				// 2. 如果失败，尝试解析为 JSON 数组
				var contentSlice []interface{}
				if err := common.Unmarshal([]byte(contentStr), &contentSlice); err == nil {
					// 如果是数组，包装成对象
					contentMap = map[string]interface{}{"result": contentSlice}
				} else {
					// 3. 如果再次失败，作为纯文本处理
					contentMap = map[string]interface{}{"content": contentStr}
				}
			}

			functionResp := &dto.GeminiFunctionResponse{
				Name:     name,
				Response: contentMap,
			}

			*parts = append(*parts, dto.GeminiPart{
				FunctionResponse: functionResp,
			})
			continue
		}
		var parts []dto.GeminiPart
		content := dto.GeminiChatContent{
			Role: message.Role,
		}
		shouldAttachThoughtSignature := attachThoughtSignature && (message.Role == "assistant" || message.Role == "model")
		signatureAttached := false
		// isToolCall := false
		if message.ToolCalls != nil {
			// message.Role = "model"
			// isToolCall = true
			for _, call := range message.ParseToolCalls() {
				args := map[string]interface{}{}
				if call.Function.Arguments != "" {
					if common.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
						return nil, fmt.Errorf("invalid arguments for function %s, args_%s", call.Function.Name, common.PayloadMetadata([]byte(call.Function.Arguments)))
					}
				}
				toolCall := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						FunctionName: call.Function.Name,
						Arguments:    args,
					},
				}
				if shouldAttachThoughtSignature && !signatureAttached && hasFunctionCallContent(toolCall.FunctionCall) && len(toolCall.ThoughtSignature) == 0 {
					toolCall.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					signatureAttached = true
				}
				parts = append(parts, toolCall)
				tool_call_ids[call.ID] = call.Function.Name
			}
		}

		openaiContent := message.ParseContent()
		for _, part := range openaiContent {
			if part.Type == dto.ContentTypeText {
				if part.Text == "" {
					continue
				}
				// check markdown image ![image](data:image/jpeg;base64,xxxxxxxxxxxx)
				// 使用字符串查找而非正则，避免大文本性能问题
				text := part.Text
				hasMarkdownImage := false
				for {
					// 快速检查是否包含 markdown 图片标记
					startIdx := strings.Index(text, "![")
					if startIdx == -1 {
						break
					}
					// 找到 ](
					bracketIdx := strings.Index(text[startIdx:], "](data:")
					if bracketIdx == -1 {
						break
					}
					bracketIdx += startIdx
					// 找到闭合的 )
					closeIdx := strings.Index(text[bracketIdx+2:], ")")
					if closeIdx == -1 {
						break
					}
					closeIdx += bracketIdx + 2

					hasMarkdownImage = true
					// 添加图片前的文本
					if startIdx > 0 {
						textBefore := text[:startIdx]
						if textBefore != "" {
							parts = append(parts, dto.GeminiPart{
								Text: textBefore,
							})
						}
					}
					// 提取 data URL (从 "](" 后面开始，到 ")" 之前)
					dataUrl := text[bracketIdx+2 : closeIdx]
					format, base64String, err := service.DecodeBase64FileData(dataUrl)
					if err != nil {
						return nil, fmt.Errorf("decode markdown base64 image data failed: %s", err.Error())
					}
					imgPart := dto.GeminiPart{
						InlineData: &dto.GeminiInlineData{
							MimeType: format,
							Data:     base64String,
						},
					}
					if shouldAttachThoughtSignature {
						imgPart.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					}
					parts = append(parts, imgPart)
					// 继续处理剩余文本
					text = text[closeIdx+1:]
				}
				// 添加剩余文本或原始文本（如果没有找到 markdown 图片）
				if !hasMarkdownImage {
					parts = append(parts, dto.GeminiPart{
						Text: part.Text,
					})
				}
			} else {
				source := part.ToFileSource()
				if source == nil {
					continue
				}
				base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Gemini")
				if err != nil {
					return nil, fmt.Errorf("get file data from source_%s failed: %w", common.PayloadMetadata([]byte(source.GetIdentifier())), err)
				}

				// 校验 MimeType 是否在 Gemini 支持的白名单中
				if _, ok := geminiSupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
					return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', source_%s, supported types are: %v", mimeType, common.PayloadMetadata([]byte(source.GetIdentifier())), getSupportedMimeTypesList())
				}

				parts = append(parts, dto.GeminiPart{
					InlineData: &dto.GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Data,
					},
				})
			}
		}

		// 如果需要附加签名但还没有附加（没有 tool_calls 或 tool_calls 为空），
		// 则在第一个文本 part 上附加 thoughtSignature
		if shouldAttachThoughtSignature && !signatureAttached && len(parts) > 0 {
			for i := range parts {
				if parts[i].Text != "" {
					parts[i].ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					break
				}
			}
		}

		content.Parts = parts

		// there's no assistant role in gemini and API shall vomit if Role is not user or model
		if content.Role == "assistant" {
			content.Role = "model"
		}
		if len(content.Parts) > 0 {
			geminiRequest.Contents = append(geminiRequest.Contents, content)
		}
	}

	if len(system_content) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{
				{
					Text: strings.Join(system_content, "\n"),
				},
			},
		}
	}

	return &geminiRequest, nil
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组
func parseStopSequences(stop any) []string {
	if stop == nil {
		return nil
	}

	switch v := stop.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []interface{}:
		sequences := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				sequences = append(sequences, str)
			}
		}
		return sequences
	}
	return nil
}

func hasFunctionCallContent(call *dto.FunctionCall) bool {
	if call == nil {
		return false
	}
	if strings.TrimSpace(call.FunctionName) != "" {
		return true
	}

	switch v := call.Arguments.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]interface{}:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	default:
		return true
	}
}

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

// cleanFunctionParameters recursively removes unsupported fields from Gemini function parameters.
func cleanFunctionParameters(params interface{}) interface{} {
	return cleanFunctionParametersWithDepth(params, 0)
}

func cleanFunctionParametersWithDepth(params interface{}, depth int) interface{} {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]interface{}:
		// Keep only Gemini-supported OpenAPI schema subset fields (per official SDK Schema).
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)

		// Clean properties
		if props, ok := cleanedMap["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{})
			for propName, propValue := range props {
				cleanedProps[propName] = cleanFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		// Recursively clean items in arrays
		if items, ok := cleanedMap["items"].(map[string]interface{}); ok && items != nil {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(items, depth+1)
		}
		// OpenAPI tuple-style items is not supported by Gemini SDK Schema; keep first to avoid API rejection.
		if itemsArray, ok := cleanedMap["items"].([]interface{}); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		// Recursively clean anyOf
		if nested, ok := cleanedMap["anyOf"].([]interface{}); ok && nested != nil {
			cleanedNested := make([]interface{}, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap

	case []interface{}:
		// Handle arrays of schemas
		cleanedArray := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray

	default:
		// Not a map or array, return as is (e.g., could be a primitive)
		return params
	}
}

func cleanFunctionParametersShallow(params interface{}) interface{} {
	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		// Stop recursion and avoid retaining huge nested structures.
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []interface{}:
		// Prefer an empty list over deep recursion on attacker-controlled inputs.
		return []interface{}{}
	default:
		return params
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]interface{}) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch t := rawType.(type) {
	case string:
		normalized, isNull := normalize(t)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []interface{}:
		nullable := false
		var chosen string
		for _, item := range t {
			if s, ok := item.(string); ok {
				normalized, isNull := normalize(s)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func removeAdditionalPropertiesWithDepth(schema interface{}, depth int) interface{} {
	if depth >= 5 {
		return schema
	}

	v, ok := schema.(map[string]interface{})
	if !ok || len(v) == 0 {
		return schema
	}
	// 删除所有的title字段
	delete(v, "title")
	delete(v, "$schema")
	// 如果type不为object和array，则直接返回
	if typeVal, exists := v["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch v["type"] {
	case "object":
		delete(v, "additionalProperties")
		// 处理 properties
		if properties, ok := v["properties"].(map[string]interface{}); ok {
			for key, value := range properties {
				properties[key] = removeAdditionalPropertiesWithDepth(value, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := v[field].([]interface{}); ok {
				for i, item := range nested {
					nested[i] = removeAdditionalPropertiesWithDepth(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := v["items"].(map[string]interface{}); ok {
			v["items"] = removeAdditionalPropertiesWithDepth(items, depth+1)
		}
	}

	return v
}

func unescapeString(s string) (string, error) {
	var result []rune
	escaped := false
	i := 0

	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:]) // 正确解码UTF-8字符
		if r == utf8.RuneError {
			return "", fmt.Errorf("invalid UTF-8 encoding")
		}

		if escaped {
			// 如果是转义符后的字符，检查其类型
			switch r {
			case '"':
				result = append(result, '"')
			case '\\':
				result = append(result, '\\')
			case '/':
				result = append(result, '/')
			case 'b':
				result = append(result, '\b')
			case 'f':
				result = append(result, '\f')
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\'':
				result = append(result, '\'')
			default:
				// 如果遇到一个非法的转义字符，直接按原样输出
				result = append(result, '\\', r)
			}
			escaped = false
		} else {
			if r == '\\' {
				escaped = true // 记录反斜杠作为转义符
			} else {
				result = append(result, r)
			}
		}
		i += size // 移动到下一个字符
	}

	return string(result), nil
}
func unescapeMapOrSlice(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			v[k] = unescapeMapOrSlice(val)
		}
	case []interface{}:
		for i, val := range v {
			v[i] = unescapeMapOrSlice(val)
		}
	case string:
		if unescaped, err := unescapeString(v); err != nil {
			return v
		} else {
			return unescaped
		}
	}
	return data
}

func getResponseToolCall(item *dto.GeminiPart) *dto.ToolCallResponse {
	var argsBytes []byte
	var err error
	// 移除 unescapeMapOrSlice 调用，直接序列化
	// JSON 序列化/反序列化已经正确处理了转义字符
	argsBytes, err = common.Marshal(item.FunctionCall.Arguments)

	if err != nil {
		return nil
	}
	return &dto.ToolCallResponse{
		ID:   fmt.Sprintf("call_%s", common.GetUUID()),
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}
}

func buildUsageFromGeminiMetadata(metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func responseGeminiChat2OpenAI(c *gin.Context, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	isToolCall := false
	for _, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: constant.FinishReasonStop,
		}
		if len(candidate.Content.Parts) > 0 {
			// 使用 strings.Builder 直接累积最终 content，避免:
			//   1) 每张 inline image 生成一次中间 "![image](...)" 字符串
			//   2) 末尾 strings.Join 再分配一份等大缓冲
			// Gemini 图片返回时 InlineData.Data 可能是数 MB 的 base64，
			// 上述两份临时分配在高并发下会显著放大堆驻留。
			var content strings.Builder
			var inlineGrow int
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
				}
			}
			if inlineGrow > 0 {
				content.Grow(inlineGrow)
			}
			appended := 0
			writeSep := func() {
				if appended > 0 {
					content.WriteByte('\n')
				}
				appended++
			}
			var toolCalls []dto.ToolCallResponse
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					// 媒体内容
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						writeSep()
						content.WriteString("![image](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					} else {
						// 其他媒体类型，直接显示链接
						writeSep()
						content.WriteString("[media](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = constant.FinishReasonToolCalls
					if call := getResponseToolCall(&part); call != nil {
						toolCalls = append(toolCalls, *call)
					}
				} else if part.Thought != nil && *part.Thought {
					choice.Message.ReasoningContent = &part.Text
				} else {
					if part.ExecutableCode != nil {
						writeSep()
						content.WriteString("```")
						content.WriteString(part.ExecutableCode.Language)
						content.WriteByte('\n')
						content.WriteString(part.ExecutableCode.Code)
						content.WriteString("\n```")
					} else if part.CodeExecutionResult != nil {
						writeSep()
						content.WriteString("```output\n")
						content.WriteString(part.CodeExecutionResult.Output)
						content.WriteString("\n```")
					} else {
						// 过滤掉空行
						if part.Text != "\n" {
							writeSep()
							content.WriteString(part.Text)
						}
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				isToolCall = true
			}
			choice.Message.SetStringContent(content.String())

		}
		if candidate.FinishReason != nil {
			switch *candidate.FinishReason {
			case "STOP":
				choice.FinishReason = constant.FinishReasonStop
			case "MAX_TOKENS":
				choice.FinishReason = constant.FinishReasonLength
			case "SAFETY":
				// Safety filter triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "RECITATION":
				// Recitation (citation) detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "BLOCKLIST":
				// Blocklist triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "PROHIBITED_CONTENT":
				// Prohibited content detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "SPII":
				// Sensitive personally identifiable information
				choice.FinishReason = constant.FinishReasonContentFilter
			case "OTHER":
				// Other reasons
				choice.FinishReason = constant.FinishReasonContentFilter
			default:
				choice.FinishReason = constant.FinishReasonContentFilter
			}
		}
		if isToolCall {
			choice.FinishReason = constant.FinishReasonToolCalls
		}

		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	choices := make([]dto.ChatCompletionsStreamResponseChoice, 0, len(geminiResponse.Candidates))
	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: int(candidate.Index),
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				//Role: "assistant",
			},
		}
		// 使用 strings.Builder 直接累积 delta content，避免每张 image / 每个
		// 文本片段都先 `+` 拼出一份临时 string，再 strings.Join 再拷贝一遍。
		var content strings.Builder
		var inlineGrow int
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
			}
		}
		if inlineGrow > 0 {
			content.Grow(inlineGrow)
		}
		appended := 0
		writeSep := func() {
			if appended > 0 {
				content.WriteByte('\n')
			}
			appended++
		}
		isTools := false
		isThought := false
		if candidate.FinishReason != nil {
			// Map Gemini FinishReason to OpenAI finish_reason
			switch *candidate.FinishReason {
			case "STOP":
				// Normal completion
				choice.FinishReason = &constant.FinishReasonStop
			case "MAX_TOKENS":
				// Reached maximum token limit
				choice.FinishReason = &constant.FinishReasonLength
			case "SAFETY":
				// Safety filter triggered
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "RECITATION":
				// Recitation (citation) detected
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "BLOCKLIST":
				// Blocklist triggered
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "PROHIBITED_CONTENT":
				// Prohibited content detected
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "SPII":
				// Sensitive personally identifiable information
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "OTHER":
				// Other reasons
				choice.FinishReason = &constant.FinishReasonContentFilter
			default:
				// Unknown reason, treat as content filter
				choice.FinishReason = &constant.FinishReasonContentFilter
			}
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				if strings.HasPrefix(part.InlineData.MimeType, "image") {
					writeSep()
					content.WriteString("![image](data:")
					content.WriteString(part.InlineData.MimeType)
					content.WriteString(";base64,")
					content.WriteString(part.InlineData.Data)
					content.WriteByte(')')
				}
			} else if part.FunctionCall != nil {
				isTools = true
				if call := getResponseToolCall(&part); call != nil {
					call.SetIndex(len(choice.Delta.ToolCalls))
					choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, *call)
				}

			} else if part.Thought != nil && *part.Thought {
				isThought = true
				writeSep()
				content.WriteString(part.Text)
			} else {
				if part.ExecutableCode != nil {
					writeSep()
					content.WriteString("```")
					content.WriteString(part.ExecutableCode.Language)
					content.WriteByte('\n')
					content.WriteString(part.ExecutableCode.Code)
					content.WriteString("\n```\n")
				} else if part.CodeExecutionResult != nil {
					writeSep()
					content.WriteString("```output\n")
					content.WriteString(part.CodeExecutionResult.Output)
					content.WriteString("\n```\n")
				} else {
					if part.Text != "\n" {
						writeSep()
						content.WriteString(part.Text)
					}
				}
			}
		}
		if isThought {
			choice.Delta.SetReasoningContent(content.String())
		} else {
			choice.Delta.SetContentString(content.String())
		}
		if isTools {
			choice.FinishReason = &constant.FinishReasonToolCalls
		}
		choices = append(choices, choice)
	}

	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Choices = choices
	return &response, isStop
}

func handleStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	err = openai.HandleStreamFormat(c, info, string(streamData), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
	if err != nil {
		return fmt.Errorf("failed to handle stream format: %w", err)
	}
	return nil
}

func handleFinalStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	return openai.HandleFinalResponse(c, info, string(streamData), resp.Id, resp.Created, resp.Model, resp.GetSystemFingerprint(), resp.Usage, false)
}

func geminiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, callback func(data string, geminiResponse *dto.GeminiChatResponse) bool) (*dto.Usage, *types.NewAPIError) {
	var usage = &dto.Usage{}
	var imageCount int
	responseText := strings.Builder{}
	var streamErr *types.NewAPIError
	seenValidResponse := false
	completed := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if apiErr := explicitGeminiResponseError(common.StringToByteSlice(data), resp.StatusCode); apiErr != nil {
			if seenValidResponse {
				service.MarkUpstreamAccepted(c)
				streamErr = relaychannel.AcceptedResponseDeliveryError()
			} else {
				streamErr = apiErr
			}
			sr.Stop(streamErr)
			return
		}
		var geminiResponse dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &geminiResponse); err != nil {
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}

		if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			if seenValidResponse {
				service.MarkUpstreamAccepted(c)
				streamErr = relaychannel.AcceptedResponseDeliveryError()
			} else {
				streamErr = explicitGeminiPromptBlock(*geminiResponse.PromptFeedback.BlockReason)
			}
			sr.Stop(streamErr)
			return
		}
		if len(geminiResponse.Candidates) == 0 && !seenValidResponse {
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		service.MarkUpstreamAccepted(c)
		if len(geminiResponse.Candidates) > 0 {
			seenValidResponse = true
		}
		for _, candidate := range geminiResponse.Candidates {
			if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
				completed = true
			}
		}

		// 统计图片数量
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && part.InlineData.MimeType != "" {
					imageCount++
				}
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}

		// 更新使用量统计
		if geminiResponse.UsageMetadata.TotalTokenCount != 0 {
			mappedUsage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
			*usage = mappedUsage
		}

		if !callback(data, &geminiResponse) {
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
		}
	})
	if streamErr != nil {
		return usage, streamErr
	}
	finishedNormally := completed || info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone
	if info.StreamStatus == nil || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() || !seenValidResponse || !finishedNormally {
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}

	if imageCount != 0 {
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = imageCount * 1400
		}
	}

	if usage.CompletionTokens <= 0 {
		if info.ReceivedResponseCount > 0 {
			usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			usage = &dto.Usage{}
		}
	}

	return usage, nil
}

func GeminiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	id := helper.GetResponseID(c)
	createAt := common.GetTimestamp()
	finishReason := constant.FinishReasonStop
	toolCallIndexByChoice := make(map[int]map[string]int)
	nextToolCallIndexByChoice := make(map[int]int)

	usage, err := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		response, isStop := streamResponseGeminiChat2OpenAI(geminiResponse)

		response.Id = id
		response.Created = createAt
		response.Model = info.UpstreamModelName
		if response.IsToolCall() {
			finishReason = constant.FinishReasonToolCalls
			if info.RelayFormat == types.RelayFormatClaude {
				for choiceIdx := range response.Choices {
					response.Choices[choiceIdx].FinishReason = nil
				}
			}
		}
		for choiceIdx := range response.Choices {
			choiceKey := response.Choices[choiceIdx].Index
			for toolIdx := range response.Choices[choiceIdx].Delta.ToolCalls {
				tool := &response.Choices[choiceIdx].Delta.ToolCalls[toolIdx]
				if tool.ID == "" {
					continue
				}
				m := toolCallIndexByChoice[choiceKey]
				if m == nil {
					m = make(map[string]int)
					toolCallIndexByChoice[choiceKey] = m
				}
				if idx, ok := m[tool.ID]; ok {
					tool.SetIndex(idx)
					continue
				}
				idx := nextToolCallIndexByChoice[choiceKey]
				nextToolCallIndexByChoice[choiceKey] = idx + 1
				m[tool.ID] = idx
				tool.SetIndex(idx)
			}
		}

		logger.LogDebug(c, "info.SendResponseCount = %d", info.SendResponseCount)
		if info.SendResponseCount == 0 {
			// send first response
			emptyResponse := helper.GenerateStartEmptyResponse(id, createAt, info.UpstreamModelName, nil)
			if response.IsToolCall() {
				if len(emptyResponse.Choices) > 0 && len(response.Choices) > 0 {
					toolCalls := response.Choices[0].Delta.ToolCalls
					copiedToolCalls := make([]dto.ToolCallResponse, len(toolCalls))
					for idx := range toolCalls {
						copiedToolCalls[idx] = toolCalls[idx]
						copiedToolCalls[idx].Function.Arguments = ""
					}
					emptyResponse.Choices[0].Delta.ToolCalls = copiedToolCalls
				}
				finishReason = constant.FinishReasonToolCalls
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					logger.LogError(c, err.Error())
					return false
				}

				response.ClearToolCalls()
				if response.IsFinished() {
					response.Choices[0].FinishReason = nil
				}
			} else {
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					logger.LogError(c, err.Error())
					return false
				}
			}
		}

		err := handleStream(c, info, response)
		if err != nil {
			logger.LogError(c, err.Error())
			return false
		}
		if isStop {
			if info.RelayFormat != types.RelayFormatClaude {
				if err := handleStream(c, info, helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason)); err != nil {
					logger.LogError(c, err.Error())
					return false
				}
			}
		}
		return true
	})

	if err != nil {
		return usage, err
	}

	response := helper.GenerateFinalUsageResponse(id, createAt, info.UpstreamModelName, *usage)
	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && !info.ClaudeConvertInfo.Done {
		response = helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason)
		response.Usage = usage
	}
	handleErr := handleFinalStream(c, info, response)
	if handleErr != nil {
		common.SysLog("send final response failed: " + handleErr.Error())
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return usage, nil
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	logger.LogPayload(c, "Gemini response body", responseBody)
	if apiErr := explicitGeminiResponseError(responseBody, resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(geminiResponse.Candidates) == 0 {
		usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			return &usage, explicitGeminiPromptBlock(*geminiResponse.PromptFeedback.BlockReason)
		}
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
		return &usage, unknownGeminiEmptyResponse()
	}
	service.MarkUpstreamAccepted(c)
	fullTextResponse := responseGeminiChat2OpenAI(c, &geminiResponse)
	fullTextResponse.Model = info.UpstreamModelName
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

	fullTextResponse.Usage = usage

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		responseBody, err = common.Marshal(fullTextResponse)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(fullTextResponse, info)
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := common.ReadAllWithLimit(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if apiErr := explicitGeminiResponseError(responseBody, resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.MarkUpstreamAccepted(c)

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var geminiResponse dto.GeminiImageResponse
	defer service.CloseResponseBodyGracefully(resp)
	if jsonErr := common.DecodeJsonWithLimit(resp.Body, &geminiResponse, common.UpstreamJSONBodyLimit()); jsonErr != nil {
		service.MarkUpstreamAccepted(c)
		common.SysError(fmt.Sprintf("accepted Gemini image response decode failed: error_type=%T", jsonErr))
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	if len(geminiResponse.Predictions) == 0 {
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}
	budget := service.NewImageResponseEncodedBudget()
	filtered := false

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			filtered = true
			continue // skip filtered image
		}
		if prediction.BytesBase64Encoded == "" {
			continue
		}
		service.MarkUpstreamAccepted(c)
		if err := budget.ConsumeBase64(prediction.BytesBase64Encoded); err != nil {
			common.SysError(fmt.Sprintf("accepted Gemini image response exceeded cumulative encoded budget: error_type=%T", err))
			return nil, relaychannel.AcceptedResponseDeliveryError()
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}
	if len(openAIResponse.Data) == 0 {
		if filtered {
			return nil, types.NewErrorWithStatusCode(
				errors.New("image request was rejected by the provider safety filter"),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if writeErr := relaychannel.WriteImageResponse(c.Writer, &openAIResponse); writeErr != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(writeErr)
		}
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
	}

	return usage, nil
}

type GeminiModelsResponse struct {
	Models        []dto.GeminiModel `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

func FetchGeminiModels(baseURL, apiKey, proxyURL string) ([]string, error) {
	return FetchGeminiModelsWithContext(context.Background(), baseURL, apiKey, proxyURL)
}

func FetchGeminiModelsWithContext(ctx context.Context, baseURL, apiKey, proxyURL string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP客户端失败: %v", err)
	}

	allModels := make([]string, 0)
	nextPageToken := ""
	maxPages := 100 // Safety limit to prevent infinite loops

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/v1beta/models", baseURL)
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建请求失败: %v", err)
		}

		request.Header.Set("x-goog-api-key", apiKey)

		response, err := client.Do(request)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("请求失败: %v", err)
		}

		if response.StatusCode != http.StatusOK {
			body, readErr := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
			response.Body.Close()
			cancel()
			if readErr != nil {
				return nil, fmt.Errorf("读取错误响应失败: %w", readErr)
			}
			return nil, fmt.Errorf("服务器返回错误 %d: response_%s", response.StatusCode, common.PayloadMetadata(body))
		}

		body, err := common.ReadAllWithLimit(response.Body, common.ControlPlaneJSONMaxBytes)
		response.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %v", err)
		}

		var modelsResponse GeminiModelsResponse
		if err = common.Unmarshal(body, &modelsResponse); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		for _, model := range modelsResponse.Models {
			modelNameValue, ok := model.Name.(string)
			if !ok {
				continue
			}
			modelName := strings.TrimPrefix(modelNameValue, "models/")
			allModels = append(allModels, modelName)
		}

		nextPageToken = modelsResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	return allModels, nil
}

// convertToolChoiceToGeminiConfig converts OpenAI tool_choice to Gemini toolConfig
// OpenAI tool_choice values:
//   - "auto": Let the model decide (default)
//   - "none": Don't call any tools
//   - "required": Must call at least one tool
//   - {"type": "function", "function": {"name": "xxx"}}: Call specific function
//
// Gemini functionCallingConfig.mode values:
//   - "AUTO": Model decides whether to call functions
//   - "NONE": Model won't call functions
//   - "ANY": Model must call at least one function
func convertToolChoiceToGeminiConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	// Handle string values: "auto", "none", "required"
	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			// Unknown string value, default to AUTO
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	// Handle object value: {"type": "function", "function": {"name": "xxx"}}
	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			// Extract function name if specified
			if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		// Unsupported map structure (type is not "function"), return nil
		return nil
	}

	// Unsupported type, return nil
	return nil
}
