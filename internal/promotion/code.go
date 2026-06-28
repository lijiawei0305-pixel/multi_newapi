package promotion

import (
	"encoding/hex"
	"io"
	"net/url"
	"strings"
)

// 注册链接的固定形态：/sign-up?channel=<prefix>_<rand>（detailed-design §2.9 / task 09）。
const (
	// SignupPath 是专属注册链接的路径部分。
	SignupPath = "/sign-up"
	// ChannelParam 是携带渠道码的查询参数名。
	ChannelParam = "channel"
	// codeSep 分隔前缀与随机段：<prefix>_<rand>。
	codeSep = "_"
)

// 前缀格式约束：非空、长度上限、字符集 [a-z0-9-]（小写；不含下划线以免与分隔符混淆）。
const (
	minPrefixLen = 1
	maxPrefixLen = 32
	// randBytes 随机段熵字节数；hex 编码后为 2×randBytes 个字符。
	randBytes = 8
)

// normalizePrefix 归一化前缀：去首尾空白 + 转小写。
func normalizePrefix(prefix string) string {
	return strings.ToLower(strings.TrimSpace(prefix))
}

// validatePrefix 校验已归一化前缀的格式；非法返回 ErrChannelPrefixInvalid。
func validatePrefix(p string) error {
	n := len(p)
	if n < minPrefixLen || n > maxPrefixLen {
		return ErrChannelPrefixInvalid
	}
	for i := 0; i < n; i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return ErrChannelPrefixInvalid
		}
	}
	return nil
}

// newChannelCode 生成 <prefix>_<rand>，随机段取自 r（默认 crypto/rand）。
// 熵源出错返回 ErrCodeGen（包裹底层错误）。
func newChannelCode(r io.Reader, prefix string) (string, error) {
	b := make([]byte, randBytes)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", ErrCodeGen.Wrap(err)
	}
	return prefix + codeSep + hex.EncodeToString(b), nil
}

// signupURL 用 channel_code 拼出专属注册链接，经 url.Values 编码确保始终合法且可被
// ParseChannelCode 还原。
func signupURL(code string) string {
	v := url.Values{}
	v.Set(ChannelParam, code)
	return SignupPath + "?" + v.Encode()
}

// ParseChannelCode 从注册链接 / 查询串中提回 channel_code。接受形如
// "/sign-up?channel=wechat_ab12"、"https://aaa.wedreamhub.com/sign-up?channel=..."、
// "?channel=..." 或裸 "channel=..." 的输入；无 channel 参数或解析失败返回 ErrChannelNotFound。
func ParseChannelCode(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[i+1:]
	}
	vals, err := url.ParseQuery(s)
	if err != nil {
		return "", ErrChannelNotFound
	}
	code := strings.TrimSpace(vals.Get(ChannelParam))
	if code == "" {
		return "", ErrChannelNotFound
	}
	return code, nil
}
