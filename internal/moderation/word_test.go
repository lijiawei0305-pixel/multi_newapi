package moderation

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestValidateWord(t *testing.T) {
	mk := func(w string, mt MatchType, a ModerationAction) BannedWord {
		return BannedWord{Word: w, MatchType: mt, Action: a, Enabled: true}
	}
	cases := []struct {
		name    string
		w       BannedWord
		wantErr bool
	}{
		{"valid-contains", mk("gun", MatchContains, ActionRemind), false},
		{"valid-exact", mk("fuck", MatchExact, ActionBlock), false},
		{"valid-regex", mk(`\bgun\b`, MatchRegex, ActionRemind), false},
		{"empty", mk("", MatchContains, ActionRemind), true},
		{"blank", mk("   ", MatchContains, ActionRemind), true},
		{"too-long", mk(strings.Repeat("a", maxWordLen+1), MatchContains, ActionRemind), true},
		{"bad-regex", mk("[unclosed", MatchRegex, ActionRemind), true},
		{"unknown-matchtype", mk("x", MatchType("fuzzy"), ActionRemind), true},
		{"unknown-action", mk("x", MatchContains, ModerationAction("warn")), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateWord(c.w)
			if c.wantErr {
				if !apperr.Is(err, "MODERATION_WORD_INVALID") {
					t.Fatalf("err = %v, want MODERATION_WORD_INVALID", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
