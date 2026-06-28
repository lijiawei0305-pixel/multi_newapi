package siteconfig

import "context"

// 共享测试夹具：可注入错误的 SiteConfigRepo / Blob 假实现、指针小工具与图片魔数样本。

// --- 指针小工具 ---

func strptr(s string) *string    { return &s }
func hmptr(m HomeMode) *HomeMode { return &m }

// --- fakeRepo：支持错误注入的 SiteConfigRepo ---

type fakeRepo struct {
	cfgs   map[int64]SiteConfig
	assets map[int64]Asset
	nextID int64

	getErr      error
	upsertErr   error
	createErr   error
	getAssetErr error
	setErr      error

	upserted *SiteConfig // 最近一次 Upsert 的快照（断言"是否落库"）
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{cfgs: map[int64]SiteConfig{}, assets: map[int64]Asset{}}
}

func (f *fakeRepo) GetConfig(_ context.Context, tenantID int64) (*SiteConfig, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	cfg, ok := f.cfgs[tenantID]
	if !ok {
		return nil, false, nil
	}
	cp := cfg
	return &cp, true, nil
}

func (f *fakeRepo) UpsertConfig(_ context.Context, cfg *SiteConfig) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := *cfg
	f.cfgs[cfg.TenantID] = cp
	f.upserted = &cp
	return nil
}

func (f *fakeRepo) CreateAsset(_ context.Context, a *Asset) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.nextID++
	a.ID = f.nextID
	if a.Status == "" {
		a.Status = AssetStatusActive
	}
	f.assets[a.ID] = *a
	return nil
}

func (f *fakeRepo) GetAsset(_ context.Context, id int64) (*Asset, error) {
	if f.getAssetErr != nil {
		return nil, f.getAssetErr
	}
	a, ok := f.assets[id]
	if !ok {
		return nil, ErrAssetNotFound
	}
	cp := a
	return &cp, nil
}

func (f *fakeRepo) SetAssetStatus(_ context.Context, id int64, s AssetStatus) error {
	if f.setErr != nil {
		return f.setErr
	}
	a, ok := f.assets[id]
	if !ok {
		return ErrAssetNotFound
	}
	a.Status = s
	f.assets[id] = a
	return nil
}

// --- fakeBlob：支持错误注入的 Blob ---

type fakeBlob struct {
	putErr error
	delErr error
}

func (b *fakeBlob) Put(_ context.Context, key string, _ []byte, _ string) (string, error) {
	if b.putErr != nil {
		return "", b.putErr
	}
	return "https://blob.test/" + key, nil
}

func (b *fakeBlob) Delete(_ context.Context, _ string) error { return b.delErr }

// --- 图片魔数样本（供上传校验单测；http.DetectContentType 据此识别）---

var (
	pngData  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	jpegData = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	webpData = append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
)
