// Package apperr 提供统一的应用错误模型 AppError{Code, Msg, HTTP}。
// 各模块只返回自己命名空间的错误码（如 TENANT_*、QUOTA_*、SUBSCRIPTION_*），
// 由入口中间件统一转 HTTP + JSON。详见 doc/detailed-design.md §6.4。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError 是全项目统一的错误类型。
type AppError struct {
	Code string // 带模块前缀的稳定错误码，如 "TENANT_NOT_FOUND"
	Msg  string // 面向人/客户端的描述
	HTTP int    // 建议的 HTTP 状态码
	Err  error  // 可选：被包装的底层错误
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

// Unwrap 支持 errors.Is/As 向下解包。
func (e *AppError) Unwrap() error { return e.Err }

// New 构造一个 AppError。
func New(code, msg string, httpStatus int) *AppError {
	return &AppError{Code: code, Msg: msg, HTTP: httpStatus}
}

// Wrap 在保持错误码的同时包装底层错误。
func (e *AppError) Wrap(err error) *AppError {
	c := *e
	c.Err = err
	return &c
}

// CodeOf 提取任意 error 的错误码；非 AppError 返回 "INTERNAL"。
func CodeOf(err error) string {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return "INTERNAL"
}

// HTTPStatusOf 提取建议 HTTP 状态；非 AppError 返回 500。
func HTTPStatusOf(err error) int {
	var ae *AppError
	if errors.As(err, &ae) && ae.HTTP != 0 {
		return ae.HTTP
	}
	return http.StatusInternalServerError
}

// Is 判断 err 是否为指定错误码。
func Is(err error, code string) bool { return CodeOf(err) == code }
