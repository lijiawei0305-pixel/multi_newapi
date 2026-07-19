package setting

import (
	"strings"
	"sync"
)

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// var SensitiveWords []string
var SensitiveWords = []string{
	"test_sensitive",
}
var sensitiveWordsMutex sync.RWMutex

func SensitiveWordsToString() string {
	sensitiveWordsMutex.RLock()
	defer sensitiveWordsMutex.RUnlock()
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	updated := make([]string, 0)
	sw := strings.Split(s, "\n")
	for _, w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			updated = append(updated, w)
		}
	}
	sensitiveWordsMutex.Lock()
	SensitiveWords = updated
	sensitiveWordsMutex.Unlock()
}

func GetSensitiveWords() []string {
	sensitiveWordsMutex.RLock()
	defer sensitiveWordsMutex.RUnlock()
	return append([]string(nil), SensitiveWords...)
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
