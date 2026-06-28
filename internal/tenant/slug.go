package tenant

// slug 长度边界。一期 slug 即二级域名 label，需符合 DNS label 约束（≤63）。
// 下限 3 为默认假设（避免过短的代理子域名），见报告默认假设。
const (
	minSlugLen = 3
	maxSlugLen = 63
)

// defaultReservedSlugs 是 proposal §9 规定的保留词，不可被代理申请。
var defaultReservedSlugs = []string{
	"www", "api", "admin", "root", "dashboard", "static", "cdn", "status", "support",
}

// ReservedSlugs 返回保留词表的副本（供配置读取/单测断言；修改返回值不影响内部）。
func ReservedSlugs() []string {
	out := make([]string, len(defaultReservedSlugs))
	copy(out, defaultReservedSlugs)
	return out
}

type slugValidator struct {
	reserved map[string]struct{}
}

// NewSlugValidator 构造带默认保留词表的纯函数校验器。
func NewSlugValidator() SlugValidator {
	m := make(map[string]struct{}, len(defaultReservedSlugs))
	for _, w := range defaultReservedSlugs {
		m[w] = struct{}{}
	}
	return &slugValidator{reserved: m}
}

// Validate 先做格式校验（ErrSlugInvalid），再查保留词（ErrSlugReserved）。
// 期望传入已归一化的小写 slug（大写视为格式非法，调用方负责归一）。
func (v *slugValidator) Validate(slug string) error {
	if !validSlugFormat(slug) {
		return ErrSlugInvalid
	}
	if _, ok := v.reserved[slug]; ok {
		return ErrSlugReserved
	}
	return nil
}

// validSlugFormat 校验 DNS label 风格：小写字母/数字/连字符，
// 不以连字符开头或结尾，长度在 [minSlugLen, maxSlugLen]。
func validSlugFormat(s string) bool {
	n := len(s)
	if n < minSlugLen || n > maxSlugLen {
		return false
	}
	for i := 0; i < n; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == n-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
