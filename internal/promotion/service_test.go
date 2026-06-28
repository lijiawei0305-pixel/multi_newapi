package promotion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// failReader 是永远报错的熵源，用于覆盖渠道码生成失败分支。
type failReader struct{ err error }

func (f failReader) Read([]byte) (int, error) { return 0, f.err }

func newService() (PromotionService, *MemRepo) {
	repo := NewMemRepo()
	return NewService(repo), repo
}

func TestCreateChannel_Success(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()

	const tenantID = int64(7)
	ch, err := svc.CreateChannel(ctx, tenantID, "微信公众号", "wechat")
	if err != nil {
		t.Fatalf("CreateChannel err = %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected assigned ID")
	}
	if ch.TenantID != tenantID {
		t.Errorf("TenantID = %d, want %d", ch.TenantID, tenantID)
	}
	if ch.Prefix != "wechat" {
		t.Errorf("Prefix = %q, want wechat", ch.Prefix)
	}
	if !strings.HasPrefix(ch.ChannelCode, "wechat"+codeSep) {
		t.Errorf("ChannelCode = %q, want wechat_ prefix", ch.ChannelCode)
	}
	if ch.RegisteredCount != 0 {
		t.Errorf("RegisteredCount = %d, want 0", ch.RegisteredCount)
	}
	if ch.CreatedAt.IsZero() || ch.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}

	// 链接形态正确且可解析回 channel_code。
	if ch.SignupURL != SignupPath+"?channel="+ch.ChannelCode {
		t.Errorf("SignupURL = %q", ch.SignupURL)
	}
	parsed, err := ParseChannelCode(ch.SignupURL)
	if err != nil || parsed != ch.ChannelCode {
		t.Fatalf("ParseChannelCode(signupURL) = %q, %v; want %q", parsed, err, ch.ChannelCode)
	}
}

func TestCreateChannel_NormalizesPrefix(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()
	ch, err := svc.CreateChannel(ctx, 1, "name", "  WeChat  ")
	if err != nil {
		t.Fatalf("CreateChannel err = %v", err)
	}
	if ch.Prefix != "wechat" {
		t.Fatalf("Prefix = %q, want normalized wechat", ch.Prefix)
	}
}

func TestCreateChannel_InvalidPrefix(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		prefix string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"bad-char", "we chat"},
		{"underscore", "wechat_x"},
		{"too-long", strings.Repeat("a", maxPrefixLen+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, repo := newService()
			_, err := svc.CreateChannel(ctx, 1, "n", c.prefix)
			if !apperr.Is(err, "CHANNEL_PREFIX_INVALID") {
				t.Fatalf("err = %v, want CHANNEL_PREFIX_INVALID", err)
			}
			if len(repo.channels) != 0 {
				t.Fatal("invalid prefix should not persist a channel")
			}
		})
	}
}

func TestCreateChannel_DuplicatePrefix(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()
	if _, err := svc.CreateChannel(ctx, 1, "n", "wechat"); err != nil {
		t.Fatalf("first CreateChannel err = %v", err)
	}
	// 同前缀（不同大小写归一后相同）再建 -> 重复。
	_, err := svc.CreateChannel(ctx, 1, "n2", "WeChat")
	if !apperr.Is(err, "CHANNEL_PREFIX_DUP") {
		t.Fatalf("err = %v, want CHANNEL_PREFIX_DUP", err)
	}
}

func TestCreateChannel_CodeGenError(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := newServiceWithRand(repo, failReader{err: errors.New("boom")})
	_, err := svc.CreateChannel(ctx, 1, "n", "wechat")
	if !apperr.Is(err, "CHANNEL_CODE_GEN") {
		t.Fatalf("err = %v, want CHANNEL_CODE_GEN", err)
	}
	if len(repo.channels) != 0 {
		t.Fatal("code-gen failure should not persist a channel")
	}
}

func TestAttributeOnSignup_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()
	ch, _ := svc.CreateChannel(ctx, 42, "n", "wechat")

	const userID = int64(1001)
	if err := svc.AttributeOnSignup(ctx, ch.ChannelCode, userID); err != nil {
		t.Fatalf("AttributeOnSignup err = %v", err)
	}

	// 归属落库：user -> tenant + channel。
	att, ok := repo.GetAttribution(ctx, userID)
	if !ok {
		t.Fatal("attribution not persisted")
	}
	if att.TenantID != 42 || att.ChannelID != ch.ID || att.ChannelCode != ch.ChannelCode {
		t.Fatalf("attribution = %+v, want tenant 42 / channel %d", att, ch.ID)
	}

	// 计数递增。
	got, _ := repo.GetChannelByCode(ctx, ch.ChannelCode)
	if got.RegisteredCount != 1 {
		t.Fatalf("RegisteredCount = %d, want 1", got.RegisteredCount)
	}
}

func TestAttributeOnSignup_NotFound(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()
	for _, code := range []string{"unknown_xyz", "", "   "} {
		if err := svc.AttributeOnSignup(ctx, code, 1); !apperr.Is(err, "CHANNEL_NOT_FOUND") {
			t.Fatalf("AttributeOnSignup(%q) err = %v, want CHANNEL_NOT_FOUND", code, err)
		}
	}
}

func TestAttributeOnSignup_IncrementsPerUser(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()
	ch, _ := svc.CreateChannel(ctx, 1, "n", "wechat")

	const n = 5
	for i := 0; i < n; i++ {
		if err := svc.AttributeOnSignup(ctx, ch.ChannelCode, int64(100+i)); err != nil {
			t.Fatalf("AttributeOnSignup err = %v", err)
		}
	}
	got, _ := repo.GetChannelByCode(ctx, ch.ChannelCode)
	if got.RegisteredCount != n {
		t.Fatalf("RegisteredCount = %d, want %d", got.RegisteredCount, n)
	}
}

func TestAttributeOnSignup_Concurrent(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()
	ch, _ := svc.CreateChannel(ctx, 1, "n", "wechat")

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			if err := svc.AttributeOnSignup(ctx, ch.ChannelCode, uid); err != nil {
				t.Errorf("AttributeOnSignup err = %v", err)
			}
		}(int64(i))
	}
	wg.Wait()

	got, _ := repo.GetChannelByCode(ctx, ch.ChannelCode)
	if got.RegisteredCount != n {
		t.Fatalf("RegisteredCount = %d, want %d", got.RegisteredCount, n)
	}
}

// AttributeOnSignup 应原样上浮 CreateAttribution 的非渠道错误。
func TestAttributeOnSignup_AttributionError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("attribution write failed")
	repo := &faultyRepo{MemRepo: NewMemRepo(), attrErr: sentinel}
	svc := NewService(repo)
	ch, _ := svc.CreateChannel(ctx, 1, "n", "wechat")
	err := svc.AttributeOnSignup(ctx, ch.ChannelCode, 1)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel propagated", err)
	}
}

// faultyRepo 让 CreateAttribution 失败，覆盖 AttributeOnSignup 的错误上浮分支。
type faultyRepo struct {
	*MemRepo
	attrErr error
}

func (f *faultyRepo) CreateAttribution(context.Context, *Attribution) error {
	return f.attrErr
}

// 编译期断言：MemRepo 满足 PromotionRepo。
var _ PromotionRepo = (*MemRepo)(nil)

func ExampleParseChannelCode() {
	code, _ := ParseChannelCode("/sign-up?channel=wechat_ab12")
	fmt.Println(code)
	// Output: wechat_ab12
}
