package siteconfig

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.10）。
// THEME_NOT_IN_PALETTE / HOME_MODE_LOCKED / ASSET_TYPE_FORBIDDEN / ASSET_TOO_LARGE
// 见设计文档；ASSET_NOT_FOUND（下架不存在素材）为本轮补充的同命名空间错误码。
var (
	// ErrThemeNotInPalette 主题色不在主站预设色板内。
	ErrThemeNotInPalette = apperr.New("THEME_NOT_IN_PALETTE", "主题色不在预设色板内", http.StatusBadRequest)
	// ErrThemePresetInvalid 主题风格不在预设列表内。
	ErrThemePresetInvalid = apperr.New("THEME_PRESET_INVALID", "主题风格不在预设列表内", http.StatusBadRequest)
	// ErrHomeModeLocked 首页模式越界：一期仅放行 default/config，custom_html 锁定。
	ErrHomeModeLocked = apperr.New("HOME_MODE_LOCKED", "首页模式一期仅支持 default/config（自定义 HTML 已锁定）", http.StatusForbidden)
	// ErrAssetTypeForbidden 上传素材类型不在白名单（仅 jpg/png/webp）。
	ErrAssetTypeForbidden = apperr.New("ASSET_TYPE_FORBIDDEN", "图片类型不被允许（仅 jpg/png/webp）", http.StatusBadRequest)
	// ErrAssetTooLarge 上传素材超过体积上限（≤2MB）。
	ErrAssetTooLarge = apperr.New("ASSET_TOO_LARGE", "图片体积超过上限（≤2MB）", http.StatusRequestEntityTooLarge)
	// ErrAssetNotFound 按 id 未找到素材（下架时）。
	ErrAssetNotFound = apperr.New("ASSET_NOT_FOUND", "素材不存在", http.StatusNotFound)
	// ErrFooterInvalid 自定义页脚含被禁止的 HTML（脚本/内联事件处理器/可执行或外链标签、
	// javascript: 等伪协议）或超过长度上限。防存储型 XSS：代理不得经站点装修注入任意 HTML。
	ErrFooterInvalid = apperr.New("FOOTER_INVALID", "页脚含被禁止的内容（脚本 / 事件处理器 / 危险标签 / 伪协议）或过长", http.StatusBadRequest)
)
