// Package gormrepo 用真实 GORM 实现 moderation.BannedWordRepo + moderation.ViolationSink
// （moderation_banned_words + moderation_content_violations 两表）。详见 doc/detailed-design.md §2.14。
//
// 键统一为 tenant_id（0=全站基础库，>0=代理/租户自有）。关键不变量：
//   - ListWords/DeleteWord 强制 WHERE tenant_id=?（scopeByTenant，越权防线）。
//   - UpsertWord 落库前过 moderation.ValidateWord（与 CRUD 入口同口径）。
//   - content_violations.matched_words 以 JSON 文本存储（跨 sqlite/mysql 可移植）。
//
// 表名 moderation_*：与 new-api 原生 model/ 不冲突。
package gormrepo

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/moderation"
)

// bannedWordRow 是 moderation_banned_words 表的 GORM 模型。
type bannedWordRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID  int64     `gorm:"column:tenant_id;not null;index:idx_mbw_tenant"`
	Word      string    `gorm:"column:word;type:varchar(255);not null"`
	MatchType string    `gorm:"column:match_type;type:varchar(16);not null;default:'contains'"`
	Action    string    `gorm:"column:action;type:varchar(16);not null;default:'remind'"`
	Enabled   bool      `gorm:"column:enabled;not null;default:true"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (bannedWordRow) TableName() string { return "moderation_banned_words" }

// violationRow 是 moderation_content_violations 表的 GORM 模型。
type violationRow struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID     int64     `gorm:"column:tenant_id;not null;index:idx_mcv_tenant"`
	UserID       int64     `gorm:"column:user_id;not null;index:idx_mcv_user"`
	TokenID      int64     `gorm:"column:token_id;not null;default:0"`
	Model        string    `gorm:"column:model;type:varchar(128);not null;default:''"`
	MatchedWords string    `gorm:"column:matched_words;type:text"` // JSON []string（脱敏后）
	Excerpt      string    `gorm:"column:excerpt;type:varchar(512);not null;default:''"`
	ActionTaken  string    `gorm:"column:action_taken;type:varchar(16);not null;default:'remind'"`
	CreatedAt    time.Time `gorm:"column:created_at;index:idx_mcv_created"`
}

// TableName 固定表名。
func (violationRow) TableName() string { return "moderation_content_violations" }

// Repo 是 moderation.BannedWordRepo + moderation.ViolationSink 的 GORM 实现。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

var (
	_ moderation.BannedWordRepo = (*Repo)(nil)
	_ moderation.ViolationSink  = (*Repo)(nil)
)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补 违禁词与违规记录两表结构。由 mtwire.Migrate 在 master 节点调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&bannedWordRow{}, &violationRow{})
}

// ListWords 返回 tenant_id 恰等于 tenantID 的全部词（含禁用），按 id 升序。
func (r *Repo) ListWords(ctx context.Context, tenantID int64) ([]moderation.BannedWord, error) {
	var rows []bannedWordRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]moderation.BannedWord, 0, len(rows))
	for i := range rows {
		out = append(out, toWord(&rows[i]))
	}
	return out, nil
}

// UpsertWord 新增（ID=0，回填 ID/CreatedAt）或按 (ID, tenant_id) 更新；落库前过 ValidateWord。
func (r *Repo) UpsertWord(ctx context.Context, w *moderation.BannedWord) error {
	if err := moderation.ValidateWord(*w); err != nil {
		return err
	}
	if w.ID == 0 {
		row := bannedWordRow{
			TenantID:  w.TenantID,
			Word:      w.Word,
			MatchType: string(w.MatchType),
			Action:    string(w.Action),
			Enabled:   w.Enabled,
			CreatedAt: r.now(),
		}
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			return err
		}
		w.ID = row.ID
		w.CreatedAt = row.CreatedAt
		return nil
	}
	res := r.db.WithContext(ctx).Model(&bannedWordRow{}).
		Where("id = ? AND tenant_id = ?", w.ID, w.TenantID).
		Updates(map[string]any{
			"word":       w.Word,
			"match_type": string(w.MatchType),
			"action":     string(w.Action),
			"enabled":    w.Enabled,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return moderation.ErrWordNotFound
	}
	return nil
}

// DeleteWord 删除某租户名下指定词；跨租户视为不存在（scopeByTenant）→ ErrWordNotFound。
func (r *Repo) DeleteWord(ctx context.Context, tenantID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&bannedWordRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return moderation.ErrWordNotFound
	}
	return nil
}

// Record 落库一条违规事件（回填 ID/CreatedAt）。
func (r *Repo) Record(ctx context.Context, ev *moderation.ViolationEvent) error {
	mw, _ := common.Marshal(ev.MatchedWords)
	row := violationRow{
		TenantID:     ev.TenantID,
		UserID:       ev.UserID,
		TokenID:      ev.TokenID,
		Model:        ev.Model,
		MatchedWords: string(mw),
		Excerpt:      ev.Excerpt,
		ActionTaken:  string(ev.ActionTaken),
		CreatedAt:    r.now(),
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	ev.ID = row.ID
	ev.CreatedAt = row.CreatedAt
	return nil
}

// ListForAdmin 按租户 + 过滤条件查违规日志（created_at 倒序）。
// tenantID>=0 限定该租户；tenantID<0 = 全租户（主站全局视图，仅 super-admin 经 AdminAuth 可达）。
func (r *Repo) ListForAdmin(ctx context.Context, tenantID int64, f moderation.ViolationFilter) ([]moderation.ViolationEvent, error) {
	q := r.db.WithContext(ctx).Model(&violationRow{})
	if tenantID >= 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if f.UserID != 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		q = q.Where("created_at <= ?", f.Until)
	}
	q = q.Order("created_at desc, id desc")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var rows []violationRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]moderation.ViolationEvent, 0, len(rows))
	for i := range rows {
		out = append(out, toViolation(&rows[i]))
	}
	return out, nil
}

func toWord(row *bannedWordRow) moderation.BannedWord {
	return moderation.BannedWord{
		ID:        row.ID,
		TenantID:  row.TenantID,
		Word:      row.Word,
		MatchType: moderation.MatchType(row.MatchType),
		Action:    moderation.ModerationAction(row.Action),
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt,
	}
}

func toViolation(row *violationRow) moderation.ViolationEvent {
	var mw []string
	if row.MatchedWords != "" {
		_ = common.Unmarshal([]byte(row.MatchedWords), &mw)
	}
	return moderation.ViolationEvent{
		ID:           row.ID,
		TenantID:     row.TenantID,
		UserID:       row.UserID,
		TokenID:      row.TokenID,
		Model:        row.Model,
		MatchedWords: mw,
		Excerpt:      row.Excerpt,
		ActionTaken:  moderation.ModerationAction(row.ActionTaken),
		CreatedAt:    row.CreatedAt,
	}
}
