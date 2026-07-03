package promotion

import (
	"context"
	"crypto/rand"
	"io"
	"strings"
)

type promotionService struct {
	repo PromotionRepo
	rand io.Reader // 渠道码熵源，默认 crypto/rand.Reader；单测可注入失败源。
}

// NewService 组装 PromotionService。持久化以接口注入，便于单测；
// 随机渠道码熵源默认取 crypto/rand。
func NewService(repo PromotionRepo) PromotionService {
	return &promotionService{repo: repo, rand: rand.Reader}
}

// newServiceWithRand 供单测注入自定义熵源（覆盖渠道码生成失败分支）。
func newServiceWithRand(repo PromotionRepo, r io.Reader) *promotionService {
	return &promotionService{repo: repo, rand: r}
}

// CreateChannel 归一化并校验前缀 -> 生成 <prefix>_<rand> 渠道码与注册链接 -> 入库。
// 前缀格式非法 -> ErrChannelPrefixInvalid；前缀重复 -> ErrChannelPrefixDup。
func (s *promotionService) CreateChannel(ctx context.Context, tenantID int64, name, prefix string) (*Channel, error) {
	p := normalizePrefix(prefix)
	if err := validatePrefix(p); err != nil {
		return nil, err
	}
	code, err := newChannelCode(s.rand, p)
	if err != nil {
		return nil, err
	}
	c := &Channel{
		TenantID:     tenantID,
		Name:         strings.TrimSpace(name),
		Prefix:       p,
		ChannelCode:  code,
		SignupURL:    signupURL(code),
		DiscountRate: 1,
	}
	if err := s.repo.CreateChannel(ctx, c); err != nil {
		return nil, err // 含 ErrChannelPrefixDup
	}
	return c, nil
}

// AttributeOnSignup 按 channelCode 查渠道 -> 绑定 user 到渠道所属 tenant+channel ->
// registered_count+1。未知渠道码 -> ErrChannelNotFound。
//
// 本轮绑定与计数为两步顺序写（内存假实现下不会半途失败）；真实实现应置于同一事务，
// 并按 user 维度做幂等（同一用户重复注册不重复计数），见报告 TODO。
func (s *promotionService) AttributeOnSignup(ctx context.Context, channelCode string, userID int64) error {
	code := strings.TrimSpace(channelCode)
	if code == "" {
		return ErrChannelNotFound
	}
	ch, err := s.repo.GetChannelByCode(ctx, code)
	if err != nil {
		return err // 含 ErrChannelNotFound
	}
	a := &Attribution{
		UserID:      userID,
		TenantID:    ch.TenantID,
		ChannelID:   ch.ID,
		ChannelCode: ch.ChannelCode,
	}
	if err := s.repo.CreateAttribution(ctx, a); err != nil {
		return err
	}
	return s.repo.IncrRegisteredCount(ctx, ch.ID)
}

// VoidChannelsByTenant 作废某租户名下的全部推广渠道（代理升级为独立档时自动调用）。薄委派仓储；
// 幂等性由仓储实现保证（重复调用 / 无渠道租户均不报错）。
func (s *promotionService) VoidChannelsByTenant(ctx context.Context, tenantID int64) error {
	return s.repo.VoidChannelsByTenant(ctx, tenantID)
}
