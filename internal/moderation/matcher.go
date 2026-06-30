package moderation

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	goahocorasick "github.com/anknown/ahocorasick"
)

// matcher 是 Matcher 的内置实现。
//
// 归一化：小写化 + 全角 ASCII/空格折叠为半角（满足 §2.14 单测策略「大小写/全半角归一」）。
// contains 走 Aho-Corasick 多模匹配（按归一词表哈希缓存机器，词表不变即复用，避免每请求重建）；
// exact 在归一文本上整串比较；regex 在原文上匹配（大小写敏感与否由词作者用 (?i) 控制），按 pattern 缓存编译。
type matcher struct {
	reCache sync.Map // pattern(string) -> compiledRe
	acCache sync.Map // dictKey(string) -> *goahocorasick.Machine
}

type compiledRe struct {
	re  *regexp.Regexp
	err error
}

// NewMatcher 构造内置匹配器。
func NewMatcher() Matcher { return &matcher{} }

// normalize 小写化并把全角 ASCII（！..～）与全角空格折叠为半角。
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E: // 全角 ！..～ -> 半角 !..~
			r -= 0xFEE0
		case r == 0x3000: // 全角空格 -> 半角空格
			r = ' '
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func (m *matcher) compile(pattern string) (*regexp.Regexp, error) {
	if v, ok := m.reCache.Load(pattern); ok {
		c := v.(compiledRe)
		return c.re, c.err
	}
	re, err := regexp.Compile(pattern)
	m.reCache.Store(pattern, compiledRe{re: re, err: err})
	return re, err
}

// acKey 对归一词表求序无关哈希，作为 AC 机器缓存键。
func acKey(dict []string) string {
	cp := append([]string(nil), dict...)
	sort.Strings(cp)
	h := fnv.New64a()
	for _, w := range cp {
		h.Write([]byte{0})
		h.Write([]byte(w))
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// getOrBuildAC 取或建给定（已归一）词表的 AC 机器，按词表哈希缓存。build 失败返回 nil。
func (m *matcher) getOrBuildAC(dict []string) *goahocorasick.Machine {
	if len(dict) == 0 {
		return nil
	}
	key := acKey(dict)
	if v, ok := m.acCache.Load(key); ok {
		return v.(*goahocorasick.Machine)
	}
	runes := make([][]rune, 0, len(dict))
	for _, w := range dict {
		runes = append(runes, []rune(w))
	}
	machine := new(goahocorasick.Machine)
	if err := machine.Build(runes); err != nil {
		return nil
	}
	actual, _ := m.acCache.LoadOrStore(key, machine)
	return actual.(*goahocorasick.Machine)
}

func (m *matcher) Match(text string, words []BannedWord) []BannedWord {
	if text == "" || len(words) == 0 {
		return nil
	}
	norm := normalize(text)
	normTrim := strings.TrimSpace(norm)

	var hits []BannedWord
	seen := make(map[int64]bool, len(words))
	add := func(w BannedWord) {
		if !seen[w.ID] {
			hits = append(hits, w)
			seen[w.ID] = true
		}
	}

	// exact/regex 内联处理；contains 收集后统一走 AC。
	var containsDict []string
	containsByNorm := map[string][]BannedWord{}
	for _, w := range words {
		switch w.MatchType {
		case MatchExact:
			if normTrim == strings.TrimSpace(normalize(w.Word)) {
				add(w)
			}
		case MatchRegex:
			if re, err := m.compile(w.Word); err == nil && re.MatchString(text) {
				add(w)
			}
		default: // MatchContains（含未知类型按 contains 兜底）
			nw := normalize(w.Word)
			if nw == "" {
				continue
			}
			if _, ok := containsByNorm[nw]; !ok {
				containsDict = append(containsDict, nw)
			}
			containsByNorm[nw] = append(containsByNorm[nw], w)
		}
	}

	if len(containsDict) > 0 {
		if machine := m.getOrBuildAC(containsDict); machine != nil {
			for _, term := range machine.MultiPatternSearch([]rune(norm), false) {
				for _, w := range containsByNorm[string(term.Word)] {
					add(w)
				}
			}
		} else {
			// AC 构建失败时退化为子串匹配，保证不漏判。
			for nw, ws := range containsByNorm {
				if strings.Contains(norm, nw) {
					for _, w := range ws {
						add(w)
					}
				}
			}
		}
	}
	return hits
}
