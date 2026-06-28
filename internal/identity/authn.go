package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// HashToken 返回 raw 令牌的 hex(sha256)，作为 TokenStore 的反查键。
// 中间件 / GORM 实现复用本函数，确保 hash 口径一致——绝不存储/比对明文。
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

const bearerPrefix = "Bearer "

// normalizeToken 左剥空白、去掉可选的 "Bearer " 方案前缀，再 trim 首尾空白。
// 默认假设：AuthenticateToken 接收已从 Authorization 头取出的令牌，
// 但容忍直接传入 "Bearer xxx" 以便中间件接入。
func normalizeToken(raw string) string {
	s := strings.TrimLeft(raw, " \t\r\n")
	if len(s) >= len(bearerPrefix) && strings.EqualFold(s[:len(bearerPrefix)], bearerPrefix) {
		s = s[len(bearerPrefix):]
	}
	return strings.TrimSpace(s)
}

type authenticator struct {
	store TokenStore
}

// NewAuthenticator 用给定 TokenStore 构造 Authenticator。
func NewAuthenticator(store TokenStore) Authenticator {
	return &authenticator{store: store}
}

func (a *authenticator) AuthenticateToken(ctx context.Context, raw string) (*appctx.Principal, error) {
	tok := normalizeToken(raw)
	if tok == "" {
		return nil, apperr.New(CodeUnauthorized, "缺少访问令牌", http.StatusUnauthorized)
	}
	rec, found, err := a.store.FindByHash(ctx, HashToken(tok))
	if err != nil {
		return nil, err // 基础设施错误，原样上浮，不吞码（detailed-design §6.4）
	}
	if !found || rec == nil {
		return nil, apperr.New(CodeTokenInvalid, "令牌无效", http.StatusUnauthorized)
	}
	// 冻结令牌 / 冻结用户的令牌不可用。
	if rec.Disabled || rec.UserDisabled {
		return nil, apperr.New(CodeTokenInvalid, "令牌已被冻结", http.StatusUnauthorized)
	}
	return &appctx.Principal{
		UserID:   rec.UserID,
		TenantID: rec.TenantID,
		Role:     rec.Role,
	}, nil
}
