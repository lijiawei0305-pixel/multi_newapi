package gemini

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func explicitGeminiResponseError(data []byte, upstreamStatus int) *types.NewAPIError {
	var response dto.GeneralErrorResponse
	if err := common.Unmarshal(data, &response); err != nil || len(response.Error) == 0 || common.GetJsonType(response.Error) == "null" {
		return nil
	}

	openAIError := response.TryToOpenAIError()
	if openAIError == nil {
		openAIError = &types.OpenAIError{
			Type:    "gemini_error",
			Message: strings.TrimSpace(response.ToMessage()),
			Code:    "gemini_error",
		}
	}
	if strings.TrimSpace(openAIError.Message) == "" {
		openAIError.Message = "Gemini returned an error"
	}
	if strings.TrimSpace(openAIError.Type) == "" {
		openAIError.Type = "gemini_error"
	}

	status := upstreamStatus
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		status = http.StatusBadGateway
		if code, err := strconv.Atoi(strings.TrimSpace(common.Interface2String(openAIError.Code))); err == nil && code >= 400 && code <= 599 {
			status = code
		}
	}
	return service.MarkExplicitUpstreamRejection(types.WithOpenAIError(*openAIError, status))
}

func explicitGeminiPromptBlock(blockReason string) *types.NewAPIError {
	return service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
		errors.New("request blocked by Gemini API: "+blockReason),
		types.ErrorCodePromptBlocked,
		http.StatusBadRequest,
	))
}

func unknownGeminiEmptyResponse() *types.NewAPIError {
	return types.NewOpenAIError(
		errors.New("empty response from Gemini API"),
		types.ErrorCodeEmptyResponse,
		http.StatusBadGateway,
	)
}
