package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"

	"github.com/gin-gonic/gin"
)

type OpenAIModel struct {
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Created    int64          `json:"created"`
	OwnedBy    string         `json:"owned_by"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		Group              string `json:"group"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func fetchModelsFromEndpoint(ctx context.Context, client *http.Client, baseURL string, channelType int, key string) ([]string, error) {
	if client == nil {
		return nil, errors.New("model fetch HTTP client is unavailable")
	}
	parsedBaseURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsedBaseURL.Host == "" || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") || parsedBaseURL.User != nil {
		return nil, errors.New("model fetch base URL is invalid")
	}
	parsedBaseURL.RawQuery = ""
	parsedBaseURL.Fragment = ""

	requestPath := "/v1/models"
	if channelType == constant.ChannelTypeOllama {
		requestPath = "/api/tags"
	} else if channelType == constant.ChannelTypeGemini {
		requestPath = "/v1beta/models"
	}
	parsedBaseURL.Path = strings.TrimRight(parsedBaseURL.Path, "/") + requestPath

	models := make([]string, 0)
	nextPageToken := ""
	for page := 0; page < 100; page++ {
		requestURL := *parsedBaseURL
		if nextPageToken != "" {
			query := requestURL.Query()
			query.Set("pageToken", nextPageToken)
			requestURL.RawQuery = query.Encode()
		}
		if err := validateControlPlaneURL(requestURL.String()); err != nil {
			return nil, errors.New("model fetch URL is blocked by fetch policy")
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return nil, errors.New("model fetch request is invalid")
		}
		if channelType == constant.ChannelTypeGemini {
			request.Header.Set("x-goog-api-key", key)
		} else if key != "" {
			request.Header.Set("Authorization", "Bearer "+key)
		}

		response, err := client.Do(request)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, errors.New("model fetch request failed")
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("model fetch returned status %d", response.StatusCode)
		}

		switch channelType {
		case constant.ChannelTypeOllama:
			var result struct {
				Models []struct {
					Name string `json:"name"`
				} `json:"models"`
			}
			err = common.DecodeJsonWithLimit(response.Body, &result, common.ControlPlaneJSONMaxBytes)
			response.Body.Close()
			if err != nil {
				return nil, errors.New("model fetch response is invalid")
			}
			for _, modelInfo := range result.Models {
				models = append(models, modelInfo.Name)
			}
			return models, nil
		case constant.ChannelTypeGemini:
			var result struct {
				Models []struct {
					Name string `json:"name"`
				} `json:"models"`
				NextPageToken string `json:"nextPageToken"`
			}
			err = common.DecodeJsonWithLimit(response.Body, &result, common.ControlPlaneJSONMaxBytes)
			response.Body.Close()
			if err != nil {
				return nil, errors.New("model fetch response is invalid")
			}
			for _, modelInfo := range result.Models {
				models = append(models, strings.TrimPrefix(modelInfo.Name, "models/"))
			}
			nextPageToken = result.NextPageToken
			if nextPageToken == "" {
				return models, nil
			}
		default:
			var result struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			err = common.DecodeJsonWithLimit(response.Body, &result, common.ControlPlaneJSONMaxBytes)
			response.Body.Close()
			if err != nil {
				return nil, errors.New("model fetch response is invalid")
			}
			for _, modelInfo := range result.Data {
				models = append(models, modelInfo.ID)
			}
			return models, nil
		}
	}
	return nil, errors.New("model fetch pagination limit exceeded")
}

func buildFetchModelsHeaders(channel *model.Channel, key string) (http.Header, error) {
	var headers http.Header
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		headers = GetClaudeAuthHeader(key)
	default:
		headers = GetAuthHeader(key)
	}

	headerOverride := channel.GetHeaderOverride()
	for k, v := range headerOverride {
		if relaychannel.IsHeaderPassthroughRuleKey(k) {
			continue
		}
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("invalid header override for key %s", k)
		}
		if strings.Contains(str, "{api_key}") {
			str = strings.ReplaceAll(str, "{api_key}", key)
		}
		headers.Set(k, str)
	}

	return headers, nil
}

func FetchUpstreamModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ids, err := fetchChannelUpstreamModelIDsWithContext(c.Request.Context(), channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型列表失败: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ids,
	})
}

func FixChannelsAbilities(c *gin.Context) {
	success, fails, err := model.FixAbility()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"success": success,
			"fails":   fails,
		},
	})
}

func FetchModels(c *gin.Context) {
	var req struct {
		BaseURL string `json:"base_url"`
		Type    int    `json:"type"`
		Key     string `json:"key"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request",
		})
		return
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[req.Type]
	}

	// remove line breaks and extra spaces.
	key := strings.TrimSpace(req.Key)
	key = strings.Split(key, "\n")[0]

	client, err := newControlPlaneHTTPClient(30 * time.Second)
	if err != nil {
		common.SysLog(fmt.Sprintf("model fetch client failed: error_type=%T", err))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取模型列表失败"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	models, err := fetchModelsFromEndpoint(ctx, client, baseURL, req.Type, key)
	if err != nil {
		common.SysLog(fmt.Sprintf("model fetch failed: error_type=%T", err))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取模型列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    models,
	})
}

func GetTagModels(c *gin.Context) {
	tag := c.Query("tag")
	if tag == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "tag不能为空",
		})
		return
	}

	channels, err := model.GetChannelsByTag(tag, false, false) // idSort=false, selectAll=false
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var longestModels string
	maxLength := 0

	// Find the longest models string among all channels with the given tag
	for _, channel := range channels {
		if channel.Models != "" {
			currentModels := strings.Split(channel.Models, ",")
			if len(currentModels) > maxLength {
				maxLength = len(currentModels)
				longestModels = channel.Models
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    longestModels,
	})
	return
}
