package oauth

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("github", &GitHubProvider{})
}

// GitHubProvider implements OAuth for GitHub
type GitHubProvider struct{}

type gitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type gitHubUser struct {
	Id    int64  `json:"id"`    // GitHub numeric ID (permanent, never changes)
	Login string `json:"login"` // GitHub username (can be changed by user)
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (p *GitHubProvider) GetName() string {
	return "GitHub"
}

func (p *GitHubProvider) IsEnabled() bool {
	return common.GetAuthRuntimeConfig().GitHubOAuthEnabled
}

func (p *GitHubProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	logger.LogDebug(ctx, "[OAuth-GitHub] ExchangeToken authorization_code_%s", logger.PayloadMetadata([]byte(code)))

	authRuntime := common.GetAuthRuntimeConfig()
	values := map[string]string{
		"client_id":     authRuntime.GitHubClientID,
		"client_secret": authRuntime.GitHubClientSecret,
		"code":          code,
	}
	jsonData, err := common.Marshal(values)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] ExchangeToken error_type=%T", err))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "GitHub"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-GitHub] ExchangeToken response status: %d", res.StatusCode)

	body, err := common.ReadAllWithLimit(res.Body, upstreamOAuthResponseMaxBytes)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] ExchangeToken read error_type=%T", err))
		return nil, err
	}
	var oAuthResponse gitHubOAuthResponse
	err = common.Unmarshal(body, &oAuthResponse)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] ExchangeToken decode error_type=%T", err))
		return nil, err
	}

	if oAuthResponse.AccessToken == "" {
		logger.LogError(ctx, "[OAuth-GitHub] ExchangeToken failed: empty access token")
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "GitHub"})
	}

	logger.LogDebug(ctx, "[OAuth-GitHub] ExchangeToken success scope_%s", logger.PayloadMetadata([]byte(oAuthResponse.Scope)))

	return &OAuthToken{
		AccessToken: oAuthResponse.AccessToken,
		TokenType:   oAuthResponse.TokenType,
		Scope:       oAuthResponse.Scope,
	}, nil
}

func (p *GitHubProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo: fetching user info")

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo error_type=%T", err))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "GitHub"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo response status: %d", res.StatusCode)

	// Check for non-200 status codes before attempting to decode
	if res.StatusCode != http.StatusOK {
		body, _ := common.ReadAllWithLimit(res.Body, upstreamOAuthResponseMaxBytes)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo failed status=%d response_%s", res.StatusCode, logger.PayloadMetadata(body)))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "GitHub"}, fmt.Sprintf("status %d", res.StatusCode))
	}

	body, err := common.ReadAllWithLimit(res.Body, upstreamOAuthResponseMaxBytes)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo read error_type=%T", err))
		return nil, err
	}
	var githubUser gitHubUser
	err = common.Unmarshal(body, &githubUser)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo decode error_type=%T", err))
		return nil, err
	}

	if githubUser.Id == 0 || githubUser.Login == "" {
		logger.LogError(ctx, "[OAuth-GitHub] GetUserInfo failed: empty id or login field")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "GitHub"})
	}

	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo success user_id_%s login_present=%t name_present=%t email_present=%t",
		logger.PayloadMetadata([]byte(strconv.FormatInt(githubUser.Id, 10))), githubUser.Login != "", githubUser.Name != "", githubUser.Email != "")

	return &OAuthUser{
		ProviderUserID: strconv.FormatInt(githubUser.Id, 10), // Use numeric ID as primary identifier
		Username:       githubUser.Login,
		DisplayName:    githubUser.Name,
		Email:          githubUser.Email,
		Extra: map[string]any{
			"legacy_id": githubUser.Login, // Store login for migration from old accounts
		},
	}, nil
}

func (p *GitHubProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsGitHubIdAlreadyTaken(providerUserID)
}

func (p *GitHubProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.GitHubId = providerUserID
	return user.FillUserByGitHubId()
}

func (p *GitHubProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}

func (p *GitHubProvider) GetProviderPrefix() string {
	return "github_"
}
