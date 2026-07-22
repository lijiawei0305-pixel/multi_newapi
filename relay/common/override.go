package common

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
)

var negativeIndexRegexp = regexp.MustCompile(`\.(-\d+)`)

const (
	paramOverrideContextRequestHeaders = "request_headers"
	paramOverrideContextHeaderOverride = "header_override"
	paramOverrideContextAuditRecorder  = "__param_override_audit_recorder"
)

var errSourceHeaderNotFound = errors.New("source header does not exist")

var paramOverrideSensitivePathPrefixes = []string{
	"model",
	"original_model",
	"upstream_model",
	"service_tier",
	"inference_geo",
	"speed",
	"messages",
	"input",
	"instructions",
	"system",
	"contents",
	"systemInstruction",
	"system_instruction",
}

type paramOverrideAuditRecorder struct {
	lines []string
}

type ConditionOperation struct {
	Path           string      `json:"path"`             // JSON路径
	Mode           string      `json:"mode"`             // full, prefix, suffix, contains, gt, gte, lt, lte
	Value          interface{} `json:"value"`            // 匹配的值
	Invert         bool        `json:"invert"`           // 反选功能，true表示取反结果
	PassMissingKey bool        `json:"pass_missing_key"` // 未获取到json key时的行为
}

type ParamOperation struct {
	Path       string               `json:"path"`
	Mode       string               `json:"mode"` // delete, set, move, copy, prepend, append, trim_prefix, trim_suffix, ensure_prefix, ensure_suffix, trim_space, to_lower, to_upper, replace, regex_replace, return_error, prune_objects, set_header, delete_header, copy_header, move_header, pass_headers, sync_fields
	Value      interface{}          `json:"value"`
	KeepOrigin bool                 `json:"keep_origin"`
	From       string               `json:"from,omitempty"`
	To         string               `json:"to,omitempty"`
	Conditions []ConditionOperation `json:"conditions,omitempty"` // 条件列表
	Logic      string               `json:"logic,omitempty"`      // AND, OR (默认OR)
}

type ParamOverrideReturnError struct {
	Message    string
	StatusCode int
	Code       string
	Type       string
	SkipRetry  bool
}

func (e *ParamOverrideReturnError) Error() string {
	if e == nil {
		return "param override return error"
	}
	if e.Message == "" {
		return "param override return error"
	}
	return e.Message
}

func AsParamOverrideReturnError(err error) (*ParamOverrideReturnError, bool) {
	if err == nil {
		return nil, false
	}
	var target *ParamOverrideReturnError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func NewAPIErrorFromParamOverride(err *ParamOverrideReturnError) *types.NewAPIError {
	if err == nil {
		return types.NewError(
			errors.New("param override return error is nil"),
			types.ErrorCodeChannelParamOverrideInvalid,
			types.ErrOptionWithSkipRetry(),
		)
	}

	statusCode := err.StatusCode
	if statusCode < http.StatusContinue || statusCode > http.StatusNetworkAuthenticationRequired {
		statusCode = http.StatusBadRequest
	}

	errorCode := err.Code
	if strings.TrimSpace(errorCode) == "" {
		errorCode = string(types.ErrorCodeInvalidRequest)
	}

	errorType := err.Type
	if strings.TrimSpace(errorType) == "" {
		errorType = "invalid_request_error"
	}

	message := strings.TrimSpace(err.Message)
	if message == "" {
		message = "request blocked by param override"
	}

	opts := make([]types.NewAPIErrorOptions, 0, 1)
	if err.SkipRetry {
		opts = append(opts, types.ErrOptionWithSkipRetry())
	}

	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    errorType,
		Code:    errorCode,
	}, statusCode, opts...)
}

func ApplyParamOverride(jsonData []byte, paramOverride map[string]interface{}, conditionContext map[string]interface{}) ([]byte, error) {
	if len(paramOverride) == 0 {
		return jsonData, nil
	}
	auditRecorder := getParamOverrideAuditRecorder(conditionContext)

	// 尝试断言为操作格式
	if operations, ok := tryParseOperations(paramOverride); ok {
		legacyOverride := buildLegacyParamOverride(paramOverride)
		workingJSON := jsonData
		var err error
		if len(legacyOverride) > 0 {
			workingJSON, err = applyOperationsLegacy(workingJSON, legacyOverride, auditRecorder)
			if err != nil {
				return nil, err
			}
		}

		// 使用新方法（基于 []byte，避免整包 string 拷贝）
		return applyOperations(workingJSON, operations, conditionContext)
	}

	// 直接使用旧方法
	return applyOperationsLegacy(jsonData, paramOverride, auditRecorder)
}

func buildLegacyParamOverride(paramOverride map[string]interface{}) map[string]interface{} {
	if len(paramOverride) == 0 {
		return nil
	}
	legacy := make(map[string]interface{}, len(paramOverride))
	for key, value := range paramOverride {
		if strings.EqualFold(strings.TrimSpace(key), "operations") {
			continue
		}
		legacy[key] = value
	}
	return legacy
}

func ApplyParamOverrideWithRelayInfo(jsonData []byte, info *RelayInfo) ([]byte, error) {
	paramOverride := getParamOverrideMap(info)
	if len(paramOverride) == 0 {
		return jsonData, nil
	}

	overrideCtx := BuildParamOverrideContext(info)
	var recorder *paramOverrideAuditRecorder
	if shouldEnableParamOverrideAudit(paramOverride) {
		recorder = &paramOverrideAuditRecorder{}
		overrideCtx[paramOverrideContextAuditRecorder] = recorder
	}
	result, err := ApplyParamOverride(jsonData, paramOverride, overrideCtx)
	if err != nil {
		return nil, err
	}
	syncRuntimeHeaderOverrideFromContext(info, overrideCtx)
	if info != nil {
		if recorder != nil {
			info.ParamOverrideAudit = recorder.lines
		} else {
			info.ParamOverrideAudit = nil
		}
	}
	return result, nil
}

func shouldEnableParamOverrideAudit(paramOverride map[string]interface{}) bool {
	if common.DebugEnabled {
		return true
	}
	if len(paramOverride) == 0 {
		return false
	}
	if operations, ok := tryParseOperations(paramOverride); ok {
		for _, operation := range operations {
			if shouldAuditParamPath(strings.TrimSpace(operation.Path)) ||
				shouldAuditParamPath(strings.TrimSpace(operation.From)) ||
				shouldAuditParamPath(strings.TrimSpace(operation.To)) {
				return true
			}
		}
		for key := range buildLegacyParamOverride(paramOverride) {
			if shouldAuditParamPath(strings.TrimSpace(key)) {
				return true
			}
		}
		return false
	}
	for key := range paramOverride {
		if shouldAuditParamPath(strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}

func getParamOverrideAuditRecorder(context map[string]interface{}) *paramOverrideAuditRecorder {
	if context == nil {
		return nil
	}
	recorder, _ := context[paramOverrideContextAuditRecorder].(*paramOverrideAuditRecorder)
	return recorder
}

func (r *paramOverrideAuditRecorder) recordOperation(mode, path, from, to string, value interface{}) {
	if r == nil {
		return
	}
	line := buildParamOverrideAuditLine(mode, path, from, to, value)
	if line == "" {
		return
	}
	if lo.Contains(r.lines, line) {
		return
	}
	r.lines = append(r.lines, line)
}

func shouldAuditParamPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if common.DebugEnabled {
		return true
	}
	for _, prefix := range paramOverrideSensitivePathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

func shouldAuditOperation(mode, path, from, to string) bool {
	if common.DebugEnabled {
		return true
	}
	for _, candidate := range []string{path, from, to} {
		if shouldAuditParamPath(candidate) {
			return true
		}
	}
	return false
}

func formatParamOverrideAuditValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return "<empty>"
	case string:
		return typed
	default:
		return common.GetJsonString(typed)
	}
}

func buildParamOverrideAuditLine(mode, path, from, to string, value interface{}) string {
	mode = strings.TrimSpace(mode)
	path = strings.TrimSpace(path)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)

	if !shouldAuditOperation(mode, path, from, to) {
		return ""
	}

	switch mode {
	case "set":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("set %s = %s", path, formatParamOverrideAuditValue(value))
	case "delete":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("delete %s", path)
	case "copy":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("copy %s -> %s", from, to)
	case "move":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("move %s -> %s", from, to)
	case "prepend":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("prepend %s with %s", path, formatParamOverrideAuditValue(value))
	case "append":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("append %s with %s", path, formatParamOverrideAuditValue(value))
	case "trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s with %s", mode, path, formatParamOverrideAuditValue(value))
	case "trim_space", "to_lower", "to_upper":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s", mode, path)
	case "replace", "regex_replace":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s from %s to %s", mode, path, from, to)
	case "set_header":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("set_header %s = %s", path, formatParamOverrideAuditValue(value))
	case "delete_header":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("delete_header %s", path)
	case "copy_header", "move_header":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("%s %s -> %s", mode, from, to)
	case "pass_headers":
		return fmt.Sprintf("pass_headers %s", formatParamOverrideAuditValue(value))
	case "sync_fields":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("sync_fields %s -> %s", from, to)
	case "return_error":
		return fmt.Sprintf("return_error %s", formatParamOverrideAuditValue(value))
	default:
		if path == "" {
			return mode
		}
		return fmt.Sprintf("%s %s", mode, path)
	}
}

func getParamOverrideMap(info *RelayInfo) map[string]interface{} {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	return info.ChannelMeta.ParamOverride
}

func getHeaderOverrideMap(info *RelayInfo) map[string]interface{} {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	return info.ChannelMeta.HeadersOverride
}

func sanitizeHeaderOverrideMap(source map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return map[string]interface{}{}
	}
	target := make(map[string]interface{}, len(source))
	for key, value := range source {
		normalizedKey := normalizeHeaderContextKey(key)
		if normalizedKey == "" {
			continue
		}
		normalizedValue := strings.TrimSpace(fmt.Sprintf("%v", value))
		if normalizedValue == "" {
			if isHeaderPassthroughRuleKeyForOverride(normalizedKey) {
				target[normalizedKey] = ""
			}
			continue
		}
		target[normalizedKey] = normalizedValue
	}
	return target
}

func isHeaderPassthroughRuleKeyForOverride(key string) bool {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return false
	}
	if key == "*" {
		return true
	}
	return strings.HasPrefix(key, "re:") || strings.HasPrefix(key, "regex:")
}

func GetEffectiveHeaderOverride(info *RelayInfo) map[string]interface{} {
	if info == nil {
		return map[string]interface{}{}
	}
	if info.UseRuntimeHeadersOverride {
		return sanitizeHeaderOverrideMap(info.RuntimeHeadersOverride)
	}
	return sanitizeHeaderOverrideMap(getHeaderOverrideMap(info))
}

func tryParseOperations(paramOverride map[string]interface{}) ([]ParamOperation, bool) {
	// 检查是否包含 "operations" 字段
	opsValue, exists := paramOverride["operations"]
	if !exists {
		return nil, false
	}

	var opMaps []map[string]interface{}
	switch ops := opsValue.(type) {
	case []interface{}:
		opMaps = make([]map[string]interface{}, 0, len(ops))
		for _, op := range ops {
			opMap, ok := op.(map[string]interface{})
			if !ok {
				return nil, false
			}
			opMaps = append(opMaps, opMap)
		}
	case []map[string]interface{}:
		opMaps = ops
	default:
		return nil, false
	}

	operations := make([]ParamOperation, 0, len(opMaps))
	for _, opMap := range opMaps {
		operation := ParamOperation{}

		// 断言必要字段
		if path, ok := opMap["path"].(string); ok {
			operation.Path = path
		}
		if mode, ok := opMap["mode"].(string); ok {
			operation.Mode = mode
		} else {
			return nil, false // mode 是必需的
		}

		// 可选字段
		if value, exists := opMap["value"]; exists {
			operation.Value = value
		}
		if keepOrigin, ok := opMap["keep_origin"].(bool); ok {
			operation.KeepOrigin = keepOrigin
		}
		if from, ok := opMap["from"].(string); ok {
			operation.From = from
		}
		if to, ok := opMap["to"].(string); ok {
			operation.To = to
		}
		if logic, ok := opMap["logic"].(string); ok {
			operation.Logic = logic
		} else {
			operation.Logic = "OR" // 默认为OR
		}

		// 解析条件
		if conditions, exists := opMap["conditions"]; exists {
			parsedConditions, err := parseConditionOperations(conditions)
			if err != nil {
				return nil, false
			}
			operation.Conditions = append(operation.Conditions, parsedConditions...)
		}

		operations = append(operations, operation)
	}
	return operations, true
}

func checkConditions(data []byte, contextJSON string, conditions []ConditionOperation, logic string) (bool, error) {
	if len(conditions) == 0 {
		return true, nil // 没有条件，直接通过
	}
	results := make([]bool, len(conditions))
	for i, condition := range conditions {
		result, err := checkSingleCondition(data, contextJSON, condition)
		if err != nil {
			return false, err
		}
		results[i] = result
	}

	if strings.ToUpper(logic) == "AND" {
		return lo.EveryBy(results, func(item bool) bool { return item }), nil
	}
	return lo.SomeBy(results, func(item bool) bool { return item }), nil
}

func checkSingleCondition(data []byte, contextJSON string, condition ConditionOperation) (bool, error) {
	// 处理负数索引
	path := processNegativeIndex(data, condition.Path)
	value := gjson.GetBytes(data, path)
	if !value.Exists() && contextJSON != "" {
		value = gjson.Get(contextJSON, condition.Path)
	}
	if !value.Exists() {
		if condition.PassMissingKey {
			return true, nil
		}
		return false, nil
	}

	// 利用gjson的类型解析
	targetBytes, err := common.Marshal(condition.Value)
	if err != nil {
		return false, fmt.Errorf("failed to marshal condition value: %v", err)
	}
	targetValue := gjson.ParseBytes(targetBytes)

	result, err := compareGjsonValues(value, targetValue, strings.ToLower(condition.Mode))
	if err != nil {
		return false, fmt.Errorf("comparison failed for path %s: %v", condition.Path, err)
	}

	if condition.Invert {
		result = !result
	}
	return result, nil
}

func processNegativeIndex(data []byte, path string) string {
	matches := negativeIndexRegexp.FindAllStringSubmatch(path, -1)

	if len(matches) == 0 {
		return path
	}

	result := path
	for _, match := range matches {
		negIndex := match[1]
		index, _ := strconv.Atoi(negIndex)

		arrayPath := strings.Split(path, negIndex)[0]
		if strings.HasSuffix(arrayPath, ".") {
			arrayPath = arrayPath[:len(arrayPath)-1]
		}

		array := gjson.GetBytes(data, arrayPath)
		if array.IsArray() {
			length := len(array.Array())
			actualIndex := length + index
			if actualIndex >= 0 && actualIndex < length {
				result = strings.Replace(result, match[0], "."+strconv.Itoa(actualIndex), 1)
			}
		}
	}

	return result
}

// compareGjsonValues 直接比较两个gjson.Result，支持所有比较模式
func compareGjsonValues(jsonValue, targetValue gjson.Result, mode string) (bool, error) {
	switch mode {
	case "full":
		return compareEqual(jsonValue, targetValue)
	case "prefix":
		return strings.HasPrefix(jsonValue.String(), targetValue.String()), nil
	case "suffix":
		return strings.HasSuffix(jsonValue.String(), targetValue.String()), nil
	case "contains":
		return strings.Contains(jsonValue.String(), targetValue.String()), nil
	case "gt":
		return compareNumeric(jsonValue, targetValue, "gt")
	case "gte":
		return compareNumeric(jsonValue, targetValue, "gte")
	case "lt":
		return compareNumeric(jsonValue, targetValue, "lt")
	case "lte":
		return compareNumeric(jsonValue, targetValue, "lte")
	default:
		return false, fmt.Errorf("unsupported comparison mode: %s", mode)
	}
}

func compareEqual(jsonValue, targetValue gjson.Result) (bool, error) {
	// 对null值特殊处理：两个都是null返回true，一个是null另一个不是返回false
	if jsonValue.Type == gjson.Null || targetValue.Type == gjson.Null {
		return jsonValue.Type == gjson.Null && targetValue.Type == gjson.Null, nil
	}

	// 对布尔值特殊处理
	if (jsonValue.Type == gjson.True || jsonValue.Type == gjson.False) &&
		(targetValue.Type == gjson.True || targetValue.Type == gjson.False) {
		return jsonValue.Bool() == targetValue.Bool(), nil
	}

	// 如果类型不同，报错
	if jsonValue.Type != targetValue.Type {
		return false, fmt.Errorf("compare for different types, got %v and %v", jsonValue.Type, targetValue.Type)
	}

	switch jsonValue.Type {
	case gjson.True, gjson.False:
		return jsonValue.Bool() == targetValue.Bool(), nil
	case gjson.Number:
		return jsonValue.Num == targetValue.Num, nil
	case gjson.String:
		return jsonValue.String() == targetValue.String(), nil
	default:
		return jsonValue.String() == targetValue.String(), nil
	}
}

func compareNumeric(jsonValue, targetValue gjson.Result, operator string) (bool, error) {
	// 只有数字类型才支持数值比较
	if jsonValue.Type != gjson.Number || targetValue.Type != gjson.Number {
		return false, fmt.Errorf("numeric comparison requires both values to be numbers, got %v and %v", jsonValue.Type, targetValue.Type)
	}

	jsonNum := jsonValue.Num
	targetNum := targetValue.Num

	switch operator {
	case "gt":
		return jsonNum > targetNum, nil
	case "gte":
		return jsonNum >= targetNum, nil
	case "lt":
		return jsonNum < targetNum, nil
	case "lte":
		return jsonNum <= targetNum, nil
	default:
		return false, fmt.Errorf("unsupported numeric operator: %s", operator)
	}
}

// BuildParamOverrideContext 提供 ApplyParamOverride 可用的上下文信息。
// 目前内置以下字段：
//   - upstream_model/model：始终为通道映射后的上游模型名。
//   - original_model：请求最初指定的模型名。
//   - request_path：请求路径
//   - is_channel_test：是否为渠道测试请求（同 is_test）。
func BuildParamOverrideContext(info *RelayInfo) map[string]interface{} {
	if info == nil {
		return nil
	}

	ctx := make(map[string]interface{})
	if info.ChannelMeta != nil && info.ChannelMeta.UpstreamModelName != "" {
		ctx["model"] = info.ChannelMeta.UpstreamModelName
		ctx["upstream_model"] = info.ChannelMeta.UpstreamModelName
	}
	if info.OriginModelName != "" {
		ctx["original_model"] = info.OriginModelName
		if _, exists := ctx["model"]; !exists {
			ctx["model"] = info.OriginModelName
		}
	}

	if info.RequestURLPath != "" {
		requestPath := info.RequestURLPath
		if requestPath != "" {
			ctx["request_path"] = requestPath
		}
	}

	ctx[paramOverrideContextRequestHeaders] = buildRequestHeadersContext(info.RequestHeaders)

	headerOverrideSource := GetEffectiveHeaderOverride(info)
	ctx[paramOverrideContextHeaderOverride] = sanitizeHeaderOverrideMap(headerOverrideSource)

	ctx["retry_index"] = info.RetryIndex
	ctx["is_retry"] = info.RetryIndex > 0
	ctx["retry"] = map[string]interface{}{
		"index":    info.RetryIndex,
		"is_retry": info.RetryIndex > 0,
	}

	if info.LastError != nil {
		code := string(info.LastError.GetErrorCode())
		errorType := string(info.LastError.GetErrorType())
		lastError := map[string]interface{}{
			"status_code": info.LastError.StatusCode,
			"message":     info.LastError.Error(),
			"code":        code,
			"error_code":  code,
			"type":        errorType,
			"error_type":  errorType,
			"skip_retry":  types.IsSkipRetryError(info.LastError),
		}
		ctx["last_error"] = lastError
		ctx["last_error_status_code"] = info.LastError.StatusCode
		ctx["last_error_message"] = info.LastError.Error()
		ctx["last_error_code"] = code
		ctx["last_error_type"] = errorType
	}

	ctx["is_channel_test"] = info.IsChannelTest
	return ctx
}
