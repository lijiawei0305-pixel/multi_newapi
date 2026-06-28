package siteconfig

import "testing"

func TestInPalette(t *testing.T) {
	cases := []struct {
		name  string
		color string
		want  bool
	}{
		{"first", "#1677ff", true},
		{"uppercase", "#1677FF", true},
		{"trim space", "  #52c41a  ", true},
		{"last", "#eb2f96", true},
		{"not in palette", "#123456", false},
		{"empty", "", false},
		{"garbage", "red", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InPalette(c.color); got != c.want {
				t.Fatalf("InPalette(%q) = %v, want %v", c.color, got, c.want)
			}
		})
	}
}

func TestDefaultPaletteSizeAndCopy(t *testing.T) {
	p := DefaultPalette()
	if len(p) < 8 || len(p) > 12 {
		t.Fatalf("palette size = %d, want within [8,12]", len(p))
	}
	for _, c := range p {
		if !InPalette(c) {
			t.Errorf("palette color %q not recognized by InPalette", c)
		}
	}
	// 返回副本：改写不得影响内部状态。
	p[0] = "#000000-mutated"
	if InPalette("#000000-mutated") {
		t.Fatal("DefaultPalette returned slice aliasing internal state")
	}
}

func TestMainSiteDefault(t *testing.T) {
	d := MainSiteDefault()
	if d.ThemeColor != defaultThemeColor {
		t.Errorf("ThemeColor = %q, want %q", d.ThemeColor, defaultThemeColor)
	}
	if !InPalette(d.ThemeColor) {
		t.Errorf("default ThemeColor %q not in palette", d.ThemeColor)
	}
	if d.HomeMode != HomeModeDefault {
		t.Errorf("HomeMode = %q, want default", d.HomeMode)
	}
	if d.TenantID != 0 {
		t.Errorf("TenantID = %d, want 0", d.TenantID)
	}
	if len(d.EnabledModules) == 0 {
		t.Error("EnabledModules empty")
	}
	// 每次返回独立实例：改一个不得影响另一个。
	d2 := MainSiteDefault()
	d.EnabledModules[0] = "mutated"
	if d2.EnabledModules[0] == "mutated" {
		t.Fatal("MainSiteDefault shares EnabledModules across instances")
	}
}

func TestHomeModeValidAndPhase1(t *testing.T) {
	cases := []struct {
		mode      HomeMode
		valid     bool
		allowedP1 bool
	}{
		{HomeModeDefault, true, true},
		{HomeModeConfig, true, true},
		{HomeModeCustomHTML, true, false},
		{HomeMode("bogus"), false, false},
		{HomeMode(""), false, false},
	}
	for _, c := range cases {
		if got := c.mode.Valid(); got != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.mode, got, c.valid)
		}
		if got := c.mode.AllowedPhase1(); got != c.allowedP1 {
			t.Errorf("%q.AllowedPhase1() = %v, want %v", c.mode, got, c.allowedP1)
		}
	}
}

func TestFileSize(t *testing.T) {
	if got := (File{Data: []byte("abcd")}).Size(); got != 4 {
		t.Fatalf("Size() = %d, want 4", got)
	}
}
