package moderation

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxWordLen = 255

// ValidateWord 校验一条违禁词规则：非空、长度上限、match_type 合法、regex 可编译、action 合法。
// 不合法返回 ErrWordInvalid。供 CRUD 入口与 gormrepo.UpsertWord 共用，保证落库前一致校验。
func ValidateWord(w BannedWord) error {
	if strings.TrimSpace(w.Word) == "" || utf8.RuneCountInString(strings.TrimSpace(w.Word)) > maxWordLen {
		return ErrWordInvalid
	}
	switch w.MatchType {
	case MatchContains, MatchExact:
	case MatchRegex:
		if _, err := regexp.Compile(w.Word); err != nil {
			return ErrWordInvalid
		}
	default:
		return ErrWordInvalid
	}
	switch w.Action {
	case ActionRemind, ActionBlock:
	default:
		return ErrWordInvalid
	}
	return nil
}
