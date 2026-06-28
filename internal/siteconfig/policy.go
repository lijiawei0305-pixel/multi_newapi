package siteconfig

import (
	"net/http"
	"path/filepath"
	"strings"
)

// MaxAssetBytes 是单个素材的体积上限（2MB）。
const MaxAssetBytes = 2 << 20 // 2 * 1024 * 1024 = 2097152

// allowedExt 是允许的上传扩展名白名单：原始扩展名 -> 规范化扩展名。
// 仅 jpg/jpeg/png/webp；jpeg 归一化为 jpg。其余（svg/html/js/exe/zip…）一律拒绝。
var allowedExt = map[string]string{
	"jpg":  "jpg",
	"jpeg": "jpg",
	"png":  "png",
	"webp": "webp",
}

// allowedMIME 是经内容嗅探（http.DetectContentType）后允许的 MIME 集合。
// 与扩展名白名单双重把关：防止把 svg/html/exe 改名为 .png 绕过。
var allowedMIME = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// ValidatePatch 纯校验 SiteConfigPatch 的受控字段（无 IO，可表驱动单测）：
//   - ThemeColor 非 nil 时必须命中预设色板，否则 THEME_NOT_IN_PALETTE；
//   - HomeMode 非 nil 时仅放行 default/config，否则 HOME_MODE_LOCKED；
//   - CustomHTML 非 nil 且非空时一期锁定，返回 HOME_MODE_LOCKED。
func ValidatePatch(in SiteConfigPatch) error {
	if in.ThemeColor != nil && !InPalette(*in.ThemeColor) {
		return ErrThemeNotInPalette
	}
	if in.HomeMode != nil && !in.HomeMode.AllowedPhase1() {
		return ErrHomeModeLocked
	}
	if in.CustomHTML != nil && strings.TrimSpace(*in.CustomHTML) != "" {
		return ErrHomeModeLocked
	}
	return nil
}

// normExt 返回去掉前导点、转小写的扩展名（如 "Photo.JPG" -> "jpg"）。
func normExt(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}

// classifyUpload 纯校验上传文件并返回规范化扩展名与嗅探得到的 MIME。
// 校验顺序：扩展名白名单 -> 体积上限 -> 内容嗅探白名单。
func classifyUpload(f File) (ext, mime string, err error) {
	canon, ok := allowedExt[normExt(f.Name)]
	if !ok {
		return "", "", ErrAssetTypeForbidden
	}
	if len(f.Data) > MaxAssetBytes {
		return "", "", ErrAssetTooLarge
	}
	mime = http.DetectContentType(f.Data)
	if !allowedMIME[mime] {
		return "", "", ErrAssetTypeForbidden
	}
	return canon, mime, nil
}

// ValidateUpload 仅做上传校验（表驱动单测入口），不产生副作用。
func ValidateUpload(f File) error {
	_, _, err := classifyUpload(f)
	return err
}
