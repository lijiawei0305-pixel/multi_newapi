package mtwire

// 钱包消耗台账 mt_wallet_consume_log：财务报表 v3「钱包消耗」口径的精确数据源（doc/finance-model-report-v3.md
// §二 + §落地改动）。只记「钱包桶(users.quota)消耗」的额度，套餐(订阅)桶消耗不入本表——由此报表能把两条
// 金流（钱包充值→apikey 消耗 / tokenplan 套餐）的消耗干净分开。此前报表只能取 logs 全量作「上界」（钱包桶
// 与套餐桶混在一起，无可靠结构化字段区分），本表补上缺口。
//
// 写入点：creditConsumeCommission（agenthook.ConsumeCommission 实现；两个结算点 service/quota.go
// PostConsumeQuota 与 service/text_quota.go 都经它）——那里已按 billingSource 甄别掉套餐桶、已解析归属
// 租户（userTenantID，与消耗透镜 users.tenant_id 同口径），本表复用之。display-only：不产生任何收益、不
// 触碰任何额度，与代理分润(ratio_markup/consume_commission)完全正交。幂等键 (user_id, request_id)：
// 重放同一消耗事件不重复入账（对齐 agent_earning_logs 以 requestID 为消耗事件幂等单元的口径）。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
)

// walletConsumeRow 一条钱包桶消耗流水。WalletQuota 为该次消耗走钱包桶的额度（quota 单位；单事件资金
// 来源单一，故 = 本次全额消耗）；(user_id, request_id) 唯一，幂等去重；(tenant_id, created_at) 覆盖报表区间扫描。
type walletConsumeRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    int64     `gorm:"column:tenant_id;not null;index:idx_wallet_consume_tenant_created,priority:1"`
	UserID      int64     `gorm:"column:user_id;not null;default:0;uniqueIndex:idx_wallet_consume_user_req,priority:1"`
	WalletQuota int64     `gorm:"column:wallet_quota;not null;default:0"`
	RequestID   string    `gorm:"column:request_id;type:varchar(128);not null;uniqueIndex:idx_wallet_consume_user_req,priority:2"`
	CreatedAt   time.Time `gorm:"column:created_at;index:idx_wallet_consume_tenant_created,priority:2"`
}

// TableName 固定表名（mt_ 前缀，不与原生表冲突）。
func (walletConsumeRow) TableName() string { return "mt_wallet_consume_log" }

// migrateWalletConsumeLog 建/补钱包消耗台账（AutoMigrate，含唯一/覆盖索引）。由 App.Migrate 调用。
func migrateWalletConsumeLog(db *gorm.DB) error { return db.AutoMigrate(&walletConsumeRow{}) }

// recordWalletConsume 落一笔钱包桶消耗流水。best-effort（绝不阻断用户请求，对齐既有 consume-log 写入的
// 「记账失败只记日志、不影响扣费」纪律）：仅在 walletQuota>0、已解析到归属租户(tenant_id>0)、有 requestID
// 时入账。(user_id, request_id) 唯一 + ON CONFLICT DO NOTHING 幂等：同一消耗事件重放不双计。
// 纯记账、不产生收益、不动额度——调用方须保证已甄别掉套餐桶消耗（billingSource!=subscription）。
func (a *App) recordWalletConsume(ctx context.Context, tenantID, userID, walletQuota int64, requestID string) {
	if walletQuota <= 0 || tenantID <= 0 || requestID == "" || len(requestID) > 128 {
		return
	}
	row := walletConsumeRow{
		TenantID:    tenantID,
		UserID:      userID,
		WalletQuota: walletQuota,
		RequestID:   requestID,
		CreatedAt:   time.Now(), // 以消费发生时刻入账（缓冲期不改），保报表时间口径准
	}
	// AGENT_HOOK_ASYNC_ENABLED 开启：进程内缓冲 + 定时批量多行 INSERT（消除每请求 1 次同步写）；
	// 关闭：维持逐请求同步写。二者皆 (user_id, request_id) 唯一 + ON CONFLICT DoNothing 幂等，重放不双计。
	if common.AgentHookAsyncEnabled && a.billing != nil {
		a.billing.enqueueConsume(row)
		return
	}
	if err := a.persistWalletConsume(ctx, tenantID, userID, walletQuota, requestID); err != nil {
		common.SysError("mtwire: recordWalletConsume failed: " + err.Error())
	}
}

func (a *App) persistWalletConsume(ctx context.Context, tenantID, userID, walletQuota int64, requestID string, occurredAt ...time.Time) error {
	if a == nil || a.DB == nil {
		return errors.New("wallet consume database is unavailable")
	}
	if walletQuota <= 0 || tenantID <= 0 || requestID == "" {
		return nil
	}
	if len(requestID) > 128 {
		return errors.New("wallet consume request id is too long")
	}
	createdAt := time.Now().UTC().Truncate(time.Millisecond)
	if len(occurredAt) > 0 && !occurredAt[0].IsZero() {
		createdAt = occurredAt[0].UTC().Truncate(time.Millisecond)
	}
	row := walletConsumeRow{TenantID: tenantID, UserID: userID, WalletQuota: walletQuota, RequestID: requestID, CreatedAt: createdAt}
	if err := a.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	var persisted walletConsumeRow
	if err := a.DB.WithContext(ctx).Where("user_id = ? AND request_id = ?", userID, requestID).First(&persisted).Error; err != nil {
		return err
	}
	if persisted.TenantID != tenantID || persisted.WalletQuota != walletQuota ||
		(len(occurredAt) > 0 && !occurredAt[0].IsZero() && !persisted.CreatedAt.UTC().Truncate(time.Millisecond).Equal(createdAt)) {
		return errors.New("wallet consume idempotency payload mismatch")
	}
	return nil
}
