package openai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func semanticUpstreamErrorStatus(status int) int {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return http.StatusBadGateway
	}
	return status
}

func meaningfulOpenAIError(err *types.OpenAIError) bool {
	return err != nil && (strings.TrimSpace(err.Message) != "" || strings.TrimSpace(err.Type) != "" || strings.TrimSpace(err.Param) != "" || err.Code != nil)
}

func explicitOpenAIRejection(openAIError *types.OpenAIError, status int) *types.NewAPIError {
	if !meaningfulOpenAIError(openAIError) {
		return nil
	}
	return service.MarkExplicitUpstreamRejection(
		types.WithOpenAIError(*openAIError, semanticUpstreamErrorStatus(status)),
	)
}

func responsesStreamExplicitRejection(response *dto.ResponsesStreamResponse, data string, status int) *types.NewAPIError {
	if response == nil {
		return nil
	}
	if response.Response != nil {
		if apiErr := explicitOpenAIRejection(response.Response.GetOpenAIError(), status); apiErr != nil {
			return apiErr
		}
	}

	var payload struct {
		Type    string `json:"type"`
		Code    any    `json:"code"`
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &payload); err == nil {
		if apiErr := explicitOpenAIRejection(dto.GetOpenAIError(payload.Error), status); apiErr != nil {
			return apiErr
		}

		eventType := strings.ToLower(strings.TrimSpace(response.Type))
		if eventType != "error" && eventType != "response.error" && eventType != "response.failed" {
			return nil
		}
		if strings.TrimSpace(payload.Message) != "" || payload.Code != nil {
			return explicitOpenAIRejection(&types.OpenAIError{
				Type: payload.Type, Message: payload.Message, Code: payload.Code,
			}, status)
		}
	}

	eventType := strings.ToLower(strings.TrimSpace(response.Type))
	if eventType != "error" && eventType != "response.error" && eventType != "response.failed" {
		return nil
	}

	return service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
		fmt.Errorf("upstream returned %s", eventType),
		types.ErrorCodeBadResponse,
		semanticUpstreamErrorStatus(status),
	))
}
