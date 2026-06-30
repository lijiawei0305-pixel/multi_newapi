package moderation

import "testing"

func bw(id int64, word string, mt MatchType) BannedWord {
	return BannedWord{ID: id, Word: word, MatchType: mt, Action: ActionRemind, Enabled: true}
}

func matchedIDs(ws []BannedWord) []int64 {
	out := make([]int64, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}

func sameSet(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[int64]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestMatcher_Match(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		words []BannedWord
		want  []int64 // 期望命中的 word ID 集合（无序）
	}{
		{"contains-hit", "I want to buy a gun today", []BannedWord{bw(1, "gun", MatchContains)}, []int64{1}},
		{"contains-miss", "hello world", []BannedWord{bw(1, "gun", MatchContains)}, nil},
		{"contains-case-insensitive", "A GUN here", []BannedWord{bw(1, "gun", MatchContains)}, []int64{1}},
		{"contains-fullwidth", "ＧＵＮ in text", []BannedWord{bw(1, "gun", MatchContains)}, []int64{1}},
		{"exact-hit", "fuck", []BannedWord{bw(1, "fuck", MatchExact)}, []int64{1}},
		{"exact-miss-as-substring", "fuck you", []BannedWord{bw(1, "fuck", MatchExact)}, nil},
		{"exact-case-and-space", "  FUCK  ", []BannedWord{bw(1, "fuck", MatchExact)}, []int64{1}},
		{"regex-hit", "a gun here", []BannedWord{bw(1, `\bgun\b`, MatchRegex)}, []int64{1}},
		{"regex-boundary-miss", "a shotgun", []BannedWord{bw(1, `\bgun\b`, MatchRegex)}, nil},
		{"regex-invalid-skipped", "anything", []BannedWord{bw(1, `[unclosed`, MatchRegex)}, nil},
		{"multi-occurrence-dedup", "gun gun gun", []BannedWord{bw(1, "gun", MatchContains)}, []int64{1}},
		{"multi-words", "gun and a bomb", []BannedWord{bw(1, "gun", MatchContains), bw(2, "bomb", MatchContains)}, []int64{1, 2}},
		{"chinese-contains", "我要买枪支弹药", []BannedWord{bw(1, "枪支", MatchContains)}, []int64{1}},
		{"mixed-types", "FUCK and 枪支", []BannedWord{bw(1, "fuck", MatchExact), bw(2, "枪支", MatchContains)}, []int64{2}},
		{"empty-words", "anything", nil, nil},
		{"empty-text", "", []BannedWord{bw(1, "gun", MatchContains)}, nil},
	}
	m := NewMatcher()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchedIDs(m.Match(c.text, c.words))
			if !sameSet(got, c.want) {
				t.Fatalf("Match(%q) matched %v, want %v", c.text, got, c.want)
			}
		})
	}
}

// 同一规则集多次 Match 结果一致（覆盖 AC 机器缓存路径）。
func TestMatcher_CachedReuse(t *testing.T) {
	m := NewMatcher()
	ws := []BannedWord{bw(1, "gun", MatchContains), bw(2, "bomb", MatchContains)}
	for i := 0; i < 3; i++ {
		got := matchedIDs(m.Match("a gun", ws))
		if !sameSet(got, []int64{1}) {
			t.Fatalf("iter %d: got %v, want [1]", i, got)
		}
	}
}

// 编译期接口断言。
var _ Matcher = (*matcher)(nil)
