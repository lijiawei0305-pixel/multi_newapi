package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

func verifyTurnstile(ctx context.Context, response, remoteIP string) (bool, error) {
	form := url.Values{
		"secret":   {common.TurnstileSecretKey},
		"response": {response},
		"remoteip": {remoteIP},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rawRes, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	defer rawRes.Body.Close()
	if rawRes.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Turnstile returned status %d", rawRes.StatusCode)
	}
	var res turnstileCheckResponse
	if err := common.DecodeJsonWithLimit(rawRes.Body, &res, common.ControlPlaneJSONMaxBytes); err != nil {
		return false, err
	}
	return res.Success, nil
}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			session := sessions.Default(c)
			turnstileChecked := session.Get("turnstile")
			if turnstileChecked != nil {
				c.Next()
				return
			}
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			verified, err := verifyTurnstile(c.Request.Context(), response, c.ClientIP())
			if err != nil {
				common.SysLog(fmt.Sprintf("Turnstile request failed: error_type=%T", err))
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验服务暂时不可用，请稍后重试！",
				})
				c.Abort()
				return
			}
			if !verified {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
			session.Set("turnstile", true)
			if err = session.Save(); err != nil {
				c.JSON(http.StatusOK, gin.H{
					"message": "无法保存会话信息，请重试",
					"success": false,
				})
				return
			}
		}
		c.Next()
	}
}
