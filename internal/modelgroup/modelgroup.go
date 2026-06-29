// Package modelgroup 是「模型分组登记表 model_groups」的领域类型 + GORM 仓储（含进程内缓存）。
//
// 背景（见 doc/detailed-design.md §2.15 · 2D 倍率）：计费倍率从一维升级为二维相乘——
//
//	最终 groupRatio = GroupRatio[UserGroup(层级)] × ( UsingGroup ∈ model_groups ? GroupRatio[UsingGroup] : 1 )
//
// 本表只负责「标记哪些 group 是模型分组」+ 元数据（描述 / 绑定渠道 / 启用 / 排序），**不存倍率**——
// 倍率的唯一真源是 new-api 原生 GroupRatio（setting/ratio_setting）。这样避免两处倍率漂移。
//
// 计费热路径（relay/helper.HandleGroupRatio 每次请求）需判定 IsModelGroup(usingGroup)，故仓储维护一份
// 进程内「enabled 模型分组名集合」缓存，写后重载；启动 / 迁移后由装配层重载一次。
package modelgroup

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// mysqlDupErrNo 是 MySQL "Duplicate entry" 的错误号（唯一键冲突）。
const mysqlDupErrNo = 1062

// 仓储错误（调用方据此翻译为模块错误码）。
var (
	// ErrNotFound 表示按 name 未命中登记。
	ErrNotFound = errors.New("model group not found")
	// ErrNameTaken 表示 name 唯一键冲突（已登记同名分组）。
	ErrNameTaken = errors.New("model group name taken")
)

// ModelGroup 是模型分组登记的领域视图。注意：**不含 ratio**——倍率真源是原生 GroupRatio。
type ModelGroup struct {
	ID   int64
	Name string
	// Deprecated: 绑定真源已迁到渠道侧（channels.group 含本分组名，多对一）。HTTP 层不再读写本字段，
	// 「服务渠道」由 mtwire 反推。列保留以免破坏迁移；仓储仍支持读写（向后兼容）。
	ChannelID   *int64
	Description string
	Enabled     bool
	Sort        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ModelGroupUpdate 是局部更新入参（按非 nil 字段更新）。ratio 不在本表，由调用方另写 GroupRatio。
type ModelGroupUpdate struct {
	// Deprecated: 见 ModelGroup.ChannelID。HTTP 层不再传此字段；保留仅为向后兼容。
	ChannelID   *int64
	Description *string
	Enabled     *bool
	Sort        *int
}

// modelGroupRow 是 model_groups 表的 GORM 模型。name 唯一；**无 ratio 列**（真源是 GroupRatio）。
// 表名 model_groups：已 grep 确认不撞 new-api 原生 model/。
type modelGroupRow struct {
	ID   int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name string `gorm:"column:name;type:varchar(64);not null;uniqueIndex:idx_model_groups_name"`
	// Deprecated: 绑定迁到渠道侧（channels.group）。列保留避免破坏迁移；不再由 HTTP 层写入。
	ChannelID   *int64    `gorm:"column:channel_id"`
	Description string    `gorm:"column:description;type:varchar(255);not null;default:''"`
	Enabled     bool      `gorm:"column:enabled;not null;default:true"`
	Sort        int       `gorm:"column:sort;not null;default:0"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名，避免 GORM 复数推断歧义。
func (modelGroupRow) TableName() string { return "model_groups" }

// Repo 是 model_groups 的 GORM 仓储 + 进程内缓存。缓存仅存「enabled 模型分组名集合」，
// 供计费热路径 IsModelGroup 免查库；任何写操作（Create/Update/Delete）后重载，启动后由装配层重载。
type Repo struct {
	db    *gorm.DB
	mu    sync.RWMutex
	cache map[string]struct{} // enabled model group names
}

// New 用已建立连接的 *gorm.DB 构造仓储（缓存初始为空，须 ReloadCache 装载）。
func New(db *gorm.DB) *Repo {
	return &Repo{db: db, cache: make(map[string]struct{})}
}

// AutoMigrate 建/补 model_groups 表（含 name 唯一索引）。GORM AutoMigrate 幂等。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&modelGroupRow{})
}

// Create 登记一个模型分组（name 唯一）。落库成功后重载缓存。name 冲突 → ErrNameTaken。
func (r *Repo) Create(ctx context.Context, mg ModelGroup) (*ModelGroup, error) {
	now := time.Now()
	row := modelGroupRow{
		Name:        strings.TrimSpace(mg.Name),
		ChannelID:   mg.ChannelID,
		Description: mg.Description,
		Enabled:     mg.Enabled,
		Sort:        mg.Sort,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// Select 显式列出全部列：避免 GORM 省略 bool 零值（Enabled=false）而误套 default:true。
	err := r.db.WithContext(ctx).
		Select("Name", "ChannelID", "Description", "Enabled", "Sort", "CreatedAt", "UpdatedAt").
		Create(&row).Error
	if err != nil {
		if isDuplicate(err) {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	_ = r.ReloadCache(ctx) // best-effort：登记已落库，缓存重载失败由下次写/启动修正
	out := toModelGroup(row)
	return &out, nil
}

// Update 按 name 局部更新元数据（channel_id/description/enabled/sort，按非 nil）。ratio 不在本表。
// 行不存在返回 ErrNotFound。落库后重载缓存（enabled 切换即时影响 IsModelGroup）。
func (r *Repo) Update(ctx context.Context, name string, u ModelGroupUpdate) error {
	fields := map[string]any{"updated_at": time.Now()}
	if u.ChannelID != nil {
		fields["channel_id"] = *u.ChannelID
	}
	if u.Description != nil {
		fields["description"] = *u.Description
	}
	if u.Enabled != nil {
		fields["enabled"] = *u.Enabled
	}
	if u.Sort != nil {
		fields["sort"] = *u.Sort
	}
	res := r.db.WithContext(ctx).Model(&modelGroupRow{}).Where("name = ?", name).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	_ = r.ReloadCache(ctx)
	return nil
}

// Delete 删除登记并重载缓存（删后 IsModelGroup=false）。行不存在返回 ErrNotFound。
func (r *Repo) Delete(ctx context.Context, name string) error {
	res := r.db.WithContext(ctx).Where("name = ?", name).Delete(&modelGroupRow{})
	if res.Error != nil {
		return res.Error
	}
	_ = r.ReloadCache(ctx)
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Get 按 name 读单条；未命中返回 ErrNotFound。
func (r *Repo) Get(ctx context.Context, name string) (*ModelGroup, error) {
	var row modelGroupRow
	if err := r.db.WithContext(ctx).Take(&row, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := toModelGroup(row)
	return &out, nil
}

// List 列出全部登记（按 sort、name 升序），供后台管理读全量（含禁用）。
func (r *Repo) List(ctx context.Context) ([]ModelGroup, error) {
	var rows []modelGroupRow
	if err := r.db.WithContext(ctx).Order("sort asc, name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ModelGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelGroup(row))
	}
	return out, nil
}

// ReloadCache 从库重载「enabled 模型分组名集合」到进程内缓存（写后 / 启动 / 迁移后调用）。
func (r *Repo) ReloadCache(ctx context.Context) error {
	var rows []modelGroupRow
	if err := r.db.WithContext(ctx).
		Select("name", "enabled").Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return err
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		set[row.Name] = struct{}{}
	}
	r.mu.Lock()
	r.cache = set
	r.mu.Unlock()
	return nil
}

// IsModelGroup 报告 name 是否为「已启用」的模型分组（计费热路径用：读进程内缓存，免查库）。
// 空名 / 未登记 / 已禁用 / 缓存未装载 → false（安全：回退仅层级，绝不误判折扣）。
func (r *Repo) IsModelGroup(name string) bool {
	if name == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.cache[name]
	return ok
}

// ListEnabled 返回缓存中的已启用模型分组名（升序，稳定输出）。
func (r *Repo) ListEnabled() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.cache))
	for n := range r.cache {
		names = append(names, n)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// toModelGroup 把 db 行映射为领域视图。
func toModelGroup(row modelGroupRow) ModelGroup {
	return ModelGroup{
		ID:          row.ID,
		Name:        row.Name,
		ChannelID:   row.ChannelID,
		Description: row.Description,
		Enabled:     row.Enabled,
		Sort:        row.Sort,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

// isDuplicate 判断唯一键冲突：优先 GORM TranslateError 归一化的 ErrDuplicatedKey，兜底 MySQL 1062。
func isDuplicate(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var myErr *driver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDupErrNo
	}
	return false
}
