package siteconfig

import (
	"net/http"
	"path/filepath"
	"regexp"
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

// MaxFooterBytes 是代理自定义页脚 HTML 的长度上限（8 KiB）。页脚只需少量链接/格式，
// 上限既防滥用又给危险模式黑名单一个有界的扫描面。
const MaxFooterBytes = 8 << 10 // 8192

// footerDangerRe 命中页脚中被禁止的危险 HTML 构造：脚本标签、可执行/外链/劫持相对路径的
// 标签（script/iframe/object/embed/style/link/meta/svg/base/form）、内联事件处理器
// （形如 `<tag ... onerror=`）、以及 javascript:/vbscript:/data:text/html 伪协议。命中即整体拒绝。
//
// 定位：这是**服务端纵深**（把「代理不得注入任意 HTML」的平台决定落在与 CustomHTML 同一个
// 校验器里，堵住「字段漏进校验表即绕过既有安全决定」的模式风险）。**权威 XSS 防线是渲染端
// DOMPurify**（web/default/src/components/layout/components/footer.tsx）——黑名单可能被
// mutation-XSS 绕过，故绝不单独依赖它；两层叠加。
var footerDangerRe = regexp.MustCompile(
	`(?i)(<\s*(script|iframe|object|embed|style|link|meta|svg|base|form)\b|` +
		`<[^>]+\son[a-z]+\s*=|` +
		`javascript\s*:|vbscript\s*:|data\s*:\s*text/html)`,
)

// validateFooter 校验代理自定义页脚：长度上限 + 危险 HTML 黑名单（默认拒绝可疑构造）。
func validateFooter(s string) error {
	if len(s) > MaxFooterBytes {
		return ErrFooterInvalid
	}
	if footerDangerRe.MatchString(s) {
		return ErrFooterInvalid
	}
	return nil
}

// ValidatePatch 纯校验 SiteConfigPatch 的受控字段（无 IO，可表驱动单测）：
//   - ThemeColor 非 nil 时必须命中预设色板，否则 THEME_NOT_IN_PALETTE；
//   - HomeMode 非 nil 时仅放行 default/config，否则 HOME_MODE_LOCKED；
//   - CustomHTML 非 nil 且非空时一期锁定，返回 HOME_MODE_LOCKED；
//   - Footer 非 nil 时校验长度与危险 HTML，否则 FOOTER_INVALID。
//
// 安全铁律（默认拒绝）：凡是**代理可写且最终可能到达 HTML/JS sink 的自由文本字段**，都必须在
// 此逐字段列举校验——不能默认放行。新增此类字段（如未来的 Announcement/公告若改为 HTML 渲染）
// 时，务必同步在这里加校验并在渲染端 DOMPurify，否则会像 Footer 一样从同一端点绕过安全决定。
func ValidatePatch(in SiteConfigPatch) error {
	if in.ThemePreset != nil && !ValidThemePreset(strings.TrimSpace(*in.ThemePreset)) {
		return ErrThemePresetInvalid
	}
	if in.ThemeColor != nil && !InPalette(*in.ThemeColor) {
		return ErrThemeNotInPalette
	}
	if in.HomeMode != nil && !in.HomeMode.AllowedPhase1() {
		return ErrHomeModeLocked
	}
	if in.CustomHTML != nil && strings.TrimSpace(*in.CustomHTML) != "" {
		return ErrHomeModeLocked
	}
	if in.Footer != nil {
		if err := validateFooter(*in.Footer); err != nil {
			return err
		}
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
