package model

import "errors"

// Common errors
var (
	ErrDatabase = errors.New("database error")
	// ErrUserQuotaInsufficient and ErrTokenQuotaInsufficient are returned by
	// the atomic reserve updates when another request has already consumed the
	// remaining finite quota. Callers must treat them as a rejected reserve,
	// not as a transient database failure.
	ErrUserQuotaInsufficient  = errors.New("user quota is insufficient")
	ErrTokenQuotaInsufficient = errors.New("token quota is insufficient")
)

// User auth errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")

// 2FA errors
var ErrTwoFANotEnabled = errors.New("2fa not enabled")
