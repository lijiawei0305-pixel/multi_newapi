package siteconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// assetService 是 AssetService 的实现，依赖 SiteConfigRepo（素材元数据）与 Blob（对象存储）。
type assetService struct {
	repo SiteConfigRepo
	blob Blob
}

// NewAssetService 构造 AssetService。
func NewAssetService(repo SiteConfigRepo, blob Blob) AssetService {
	return &assetService{repo: repo, blob: blob}
}

// Upload 校验素材（类型/大小，纯校验优先）-> 自动重命名（crypto/rand，不保留原名）->
// 按 tenant 路径写对象存储 -> 记录素材元数据 -> 返回可访问 URL。
func (a *assetService) Upload(ctx context.Context, tenantID int64, f File) (string, error) {
	ext, mime, err := classifyUpload(f)
	if err != nil {
		return "", err
	}
	name, err := randomAssetName(ext)
	if err != nil {
		return "", err
	}
	key := assetKey(tenantID, name)
	url, err := a.blob.Put(ctx, key, f.Data, mime)
	if err != nil {
		return "", err
	}
	rec := &Asset{
		TenantID:    tenantID,
		Key:         key,
		URL:         url,
		ContentType: mime,
		Size:        int64(len(f.Data)),
		Status:      AssetStatusActive,
	}
	if err := a.repo.CreateAsset(ctx, rec); err != nil {
		return "", err
	}
	return url, nil
}

// Takedown 下架素材：按 ID 反查 Key -> 删对象 -> 置状态 takendown。下架后对象不可访问。
func (a *assetService) Takedown(ctx context.Context, assetID int64) error {
	rec, err := a.repo.GetAsset(ctx, assetID)
	if err != nil {
		return err // 含 ErrAssetNotFound
	}
	if err := a.blob.Delete(ctx, rec.Key); err != nil {
		return err
	}
	return a.repo.SetAssetStatus(ctx, assetID, AssetStatusTakenDown)
}

// assetKey 生成绑定 tenant 的对象存储键（强制 tenant_id 隔离前缀）。
func assetKey(tenantID int64, name string) string {
	return fmt.Sprintf("tenants/%d/assets/%s", tenantID, name)
}

// randomAssetName 用 crypto/rand 生成不可预测的随机文件名（16 字节 -> 32 hex），不保留原名。
func randomAssetName(ext string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]) + "." + ext, nil
}
