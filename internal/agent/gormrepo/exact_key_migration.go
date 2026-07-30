package gormrepo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
)

const (
	legacyWithdrawalRequestKeyIndex = "idx_agent_withdrawals_tenant_request_key"
	legacyEarningSourceIndex        = "idx_agent_earnings_source"
	legacyEarningIdemIndex          = "idx_agent_earnings_idem"
	exactKeyHashesMigrationKey      = "exact_key_hashes_v4"
)

// migrateAgentExactKeyHashes makes request and payout idempotency independent
// of the database's default text collation. It verifies both older claim
// namespaces and claims every canonical historical payout reference in v3
// before the application starts serving traffic.
func migrateAgentExactKeyHashes(db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		claimToken := common.GetUUID()
		marker := agentSchemaMigrationRow{
			Key:         exactKeyHashesMigrationKey,
			ClaimToken:  claimToken,
			CompletedAt: time.Now(),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return fmt.Errorf("claim agent exact-key migration: %w", err)
		}
		var persisted agentSchemaMigrationRow
		if err := tx.Where(&agentSchemaMigrationRow{Key: exactKeyHashesMigrationKey}).Take(&persisted).Error; err != nil {
			return fmt.Errorf("read agent exact-key migration claim: %w", err)
		}
		if persisted.ClaimToken != claimToken {
			return nil
		}

		lastEarningID := int64(0)
		for {
			var rows []earningRow
			if err := tx.Select("id", "tenant_id", "source_type", "source_id", "idem_key_hash").
				Where("id > ?", lastEarningID).Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
				return fmt.Errorf("read agent earning exact-key backfill: %w", err)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				expectedHash := (agent.EarningEntry{
					TenantID: row.TenantID, SourceType: agent.EarningSource(row.SourceType), SourceID: row.SourceID,
				}).IdempotencyKey()
				if row.IdemKeyHash == nil || *row.IdemKeyHash != expectedHash {
					if err := tx.Model(&earningRow{}).Where("id = ?", row.ID).Update("idem_key_hash", expectedHash).Error; err != nil {
						return fmt.Errorf("backfill agent earning %d exact key: %w", row.ID, err)
					}
				}
				lastEarningID = row.ID
			}
		}

		lastWithdrawalID := int64(0)
		for {
			var rows []withdrawalRow
			err := tx.Select("id", "status", "request_key", "request_key_hash", "payout_ref", "payout_ref_hash").
				Where("id > ?", lastWithdrawalID).
				Order("id ASC").Limit(500).Find(&rows).Error
			if err != nil {
				return fmt.Errorf("read agent withdrawal exact-key backfill: %w", err)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				updates := make(map[string]interface{}, 4)
				canonicalRequestKey := ""
				if row.RequestKey != nil {
					canonicalRequestKey = strings.TrimSpace(*row.RequestKey)
				}
				if canonicalRequestKey == "" {
					if row.RequestKey != nil {
						updates["request_key"] = nil
					}
					if row.RequestKeyHash != nil {
						updates["request_key_hash"] = nil
					}
				} else {
					expectedHash := exactStringHash(canonicalRequestKey)
					if row.RequestKey == nil || *row.RequestKey != canonicalRequestKey {
						updates["request_key"] = canonicalRequestKey
					}
					if row.RequestKeyHash == nil || *row.RequestKeyHash != expectedHash {
						updates["request_key_hash"] = expectedHash
					}
				}

				canonicalPayoutRef := strings.TrimSpace(row.PayoutRef)
				if row.PayoutRef != "" && canonicalPayoutRef == "" {
					return fmt.Errorf("withdrawal %d has a whitespace-only payout reference", row.ID)
				}
				if canonicalPayoutRef == "" {
					if row.PayoutRefHash != nil {
						updates["payout_ref_hash"] = nil
					}
				} else {
					expectedHash := exactStringHash(canonicalPayoutRef)
					if row.PayoutRef != canonicalPayoutRef {
						updates["payout_ref"] = canonicalPayoutRef
					}
					if row.PayoutRefHash == nil || *row.PayoutRefHash != expectedHash {
						updates["payout_ref_hash"] = expectedHash
					}
				}
				if len(updates) != 0 {
					if err := tx.Model(&withdrawalRow{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
						return fmt.Errorf("backfill agent withdrawal %d exact keys: %w", row.ID, err)
					}
				}
				lastWithdrawalID = row.ID
			}
		}

		var payoutRows []withdrawalRow
		if err := tx.Select("id", "status", "payout_ref", "payout_ref_hash", "created_at").
			Where("payout_ref <> ''").Order("id ASC").Find(&payoutRows).Error; err != nil {
			return fmt.Errorf("read historical payout references: %w", err)
		}
		payoutByWithdrawal := make(map[int64]withdrawalRow, len(payoutRows))
		for _, row := range payoutRows {
			if row.Status != string(agent.WithdrawPaid) {
				return fmt.Errorf("non-paid withdrawal %d has a payout reference", row.ID)
			}
			expectedHash := exactStringHash(row.PayoutRef)
			if row.PayoutRefHash == nil || *row.PayoutRefHash != expectedHash {
				return fmt.Errorf("withdrawal %d has an invalid payout reference hash after backfill", row.ID)
			}
			payoutByWithdrawal[row.ID] = row
		}
		var paidWithoutReference []withdrawalRow
		if err := tx.Select("id").Where("status = ? AND payout_ref = ''", string(agent.WithdrawPaid)).
			Limit(1).Find(&paidWithoutReference).Error; err != nil {
			return fmt.Errorf("validate historical paid withdrawals: %w", err)
		}
		if len(paidWithoutReference) != 0 {
			return fmt.Errorf("paid withdrawal %d has no payout reference", paidWithoutReference[0].ID)
		}

		validateLegacyClaim := func(payoutRef string, withdrawalID int64, source string) error {
			canonicalRef := strings.TrimSpace(payoutRef)
			if canonicalRef == "" {
				return fmt.Errorf("%s payout reference claim for withdrawal %d is empty", source, withdrawalID)
			}
			withdrawal, ok := payoutByWithdrawal[withdrawalID]
			if !ok {
				return fmt.Errorf("%s payout reference claim %q has no paid withdrawal %d", source, canonicalRef, withdrawalID)
			}
			if withdrawal.PayoutRef != canonicalRef {
				return fmt.Errorf("%s payout reference claim %q does not match withdrawal %d reference %q", source, canonicalRef, withdrawalID, withdrawal.PayoutRef)
			}
			return nil
		}

		// Older namespaces remain read-only migration evidence. Their stored
		// hashes are deliberately not trusted; canonical raw references must
		// agree with a paid withdrawal before v3 ownership is established.
		if tx.Migrator().HasTable(&legacyPayoutRefClaimRow{}) {
			var legacyClaims []legacyPayoutRefClaimRow
			if err := tx.Order("withdrawal_id ASC").Find(&legacyClaims).Error; err != nil {
				return fmt.Errorf("read legacy payout reference claims: %w", err)
			}
			for _, legacy := range legacyClaims {
				if err := validateLegacyClaim(legacy.PayoutRef, legacy.WithdrawalID, "legacy"); err != nil {
					return err
				}
			}
		}

		if tx.Migrator().HasTable(&v2PayoutRefClaimRow{}) {
			var v2Claims []v2PayoutRefClaimRow
			if err := tx.Order("withdrawal_id ASC").Find(&v2Claims).Error; err != nil {
				return fmt.Errorf("read v2 payout reference claims: %w", err)
			}
			for _, claim := range v2Claims {
				if err := validateLegacyClaim(claim.PayoutRef, claim.WithdrawalID, "v2"); err != nil {
					return err
				}
			}
		}
		var existingV3Claims []payoutRefClaimRow
		if err := tx.Order("withdrawal_id ASC").Find(&existingV3Claims).Error; err != nil {
			return fmt.Errorf("read existing v3 payout reference claims: %w", err)
		}
		for _, claim := range existingV3Claims {
			if err := validateLegacyClaim(claim.PayoutRef, claim.WithdrawalID, "v3"); err != nil {
				return err
			}
			if claim.PayoutRefHash != exactStringHash(strings.TrimSpace(claim.PayoutRef)) {
				return fmt.Errorf("v3 payout reference claim for withdrawal %d has an invalid hash", claim.WithdrawalID)
			}
		}

		for _, row := range payoutRows {
			claim := payoutRefClaimRow{
				PayoutRefHash: *row.PayoutRefHash,
				PayoutRef:     row.PayoutRef,
				WithdrawalID:  row.ID,
				CreatedAt:     row.CreatedAt,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim).Error; err != nil {
				return fmt.Errorf("claim canonical payout reference for withdrawal %d: %w", row.ID, err)
			}
			var owned payoutRefClaimRow
			if err := tx.Where("payout_ref_hash = ?", *row.PayoutRefHash).Take(&owned).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("read canonical payout reference claim for withdrawal %d: %w", row.ID, err)
				}
				var byWithdrawal payoutRefClaimRow
				if claimErr := tx.Where("withdrawal_id = ?", row.ID).Take(&byWithdrawal).Error; claimErr == nil {
					return fmt.Errorf("withdrawal %d already claims payout reference %q", row.ID, byWithdrawal.PayoutRef)
				} else if !errors.Is(claimErr, gorm.ErrRecordNotFound) {
					return fmt.Errorf("read payout claim ownership for withdrawal %d: %w", row.ID, claimErr)
				}
				return fmt.Errorf("canonical payout reference claim disappeared for withdrawal %d", row.ID)
			}
			if owned.WithdrawalID != row.ID || owned.PayoutRef != row.PayoutRef {
				return fmt.Errorf("payout reference %q is already associated with withdrawal %d", row.PayoutRef, owned.WithdrawalID)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// A pre-hash build used the raw request key in a collation-sensitive
	// unique index. Drop it only after every non-empty key has a hash and the
	// replacement unique index exists.
	if db.Migrator().HasIndex(&withdrawalRow{}, legacyWithdrawalRequestKeyIndex) {
		if err := db.Migrator().DropIndex(&withdrawalRow{}, legacyWithdrawalRequestKeyIndex); err != nil {
			if db.Migrator().HasIndex(&withdrawalRow{}, legacyWithdrawalRequestKeyIndex) {
				return fmt.Errorf("drop legacy withdrawal request-key index: %w", err)
			}
		}
	}
	if db.Migrator().HasIndex(&earningRow{}, legacyEarningSourceIndex) {
		if err := db.Migrator().DropIndex(&earningRow{}, legacyEarningSourceIndex); err != nil {
			if db.Migrator().HasIndex(&earningRow{}, legacyEarningSourceIndex) {
				return fmt.Errorf("drop legacy earning source index: %w", err)
			}
		}
	}
	if db.Migrator().HasIndex(&earningRow{}, legacyEarningIdemIndex) {
		if err := db.Migrator().DropIndex(&earningRow{}, legacyEarningIdemIndex); err != nil {
			if db.Migrator().HasIndex(&earningRow{}, legacyEarningIdemIndex) {
				return fmt.Errorf("drop legacy earning idempotency index: %w", err)
			}
		}
	}
	return nil
}
