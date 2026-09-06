package model

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"sort"
	"sync"
	"time"
)

var releaseFlushMu sync.Mutex
var releaseFlushErr error

func InstallReleaseWriteGuards() error {
	if !common.ReleaseDrainEnabled {
		return nil
	}
	for _, db := range []*gorm.DB{DB, LOG_DB} {
		if db == nil {
			return errors.New("database not initialized")
		}
		mark := func(tx *gorm.DB) {
			if tx.Error != nil {
				common.ReleaseWriteFailed()
			}
		}
		if err := db.Callback().Create().After("gorm:commit_or_rollback_transaction").Register("release:write_guard", mark); err != nil {
			return err
		}
		if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("release:write_guard", mark); err != nil {
			return err
		}
		if err := db.Callback().Delete().After("gorm:commit_or_rollback_transaction").Register("release:write_guard", mark); err != nil {
			return err
		}
		if err := db.Callback().Raw().After("gorm:raw").Register("release:write_guard", mark); err != nil {
			return err
		}
		if db == LOG_DB {
			break
		}
	}
	return nil
}

// Keep the original batch until commit succeeds. An ambiguous commit is
// latched, never automatically replayed: replay could charge twice.
func FlushReleaseBatch() error {
	releaseFlushMu.Lock()
	defer releaseFlushMu.Unlock()
	if releaseFlushErr != nil {
		return releaseFlushErr
	}
	for i := range batchUpdateLocks {
		batchUpdateLocks[i].Lock()
	}
	defer func() {
		for i := len(batchUpdateLocks) - 1; i >= 0; i-- {
			batchUpdateLocks[i].Unlock()
		}
	}()
	pending := 0
	for _, s := range batchUpdateStores {
		pending += len(s)
	}
	if pending == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for kind, store := range batchUpdateStores {
			ids := make([]int, 0, len(store))
			for id := range store {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for _, id := range ids {
				value := store[id]
				var result *gorm.DB
				switch kind {
				case BatchUpdateTypeUserQuota:
					result = tx.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", value))
				case BatchUpdateTypeTokenQuota:
					result = tx.Model(&Token{}).Where("id = ?", id).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota + ?", value), "used_quota": gorm.Expr("used_quota - ?", value), "accessed_time": common.GetTimestamp()})
				case BatchUpdateTypeUsedQuota:
					result = tx.Model(&User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", value))
				case BatchUpdateTypeRequestCount:
					result = tx.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", value))
				case BatchUpdateTypeChannelUsedQuota:
					result = tx.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", value))
				default:
					return errors.New("unknown batch type")
				}
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return errors.New("batch target row mismatch")
				}
			}
		}
		return nil
	})
	if err != nil {
		releaseFlushErr = errors.New("batch commit failed or uncertain; reconciliation required")
		common.ReleaseWriteFailed()
		return releaseFlushErr
	}
	for i := range batchUpdateStores {
		batchUpdateStores[i] = make(map[int]int)
	}
	return nil
}

func FlushReleaseQuotaData() error {
	releaseFlushMu.Lock()
	defer releaseFlushMu.Unlock()
	if releaseFlushErr != nil {
		return releaseFlushErr
	}
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	if len(CacheQuotaData) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize new-version dashboard writers across PostgreSQL instances.
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(714925601)").Error; err != nil {
				return err
			}
		}
		for _, q := range CacheQuotaData {
			var row QuotaData
			result := tx.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?", q.UserID, q.Username, q.ModelName, q.CreatedAt).First(&row)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				copy := *q
				if err := tx.Create(&copy).Error; err != nil {
					return err
				}
				continue
			}
			if result.Error != nil {
				return result.Error
			}
			result = tx.Model(&QuotaData{}).Where("id = ?", row.Id).Updates(map[string]interface{}{"count": gorm.Expr("count + ?", q.Count), "quota": gorm.Expr("quota + ?", q.Quota), "token_used": gorm.Expr("token_used + ?", q.TokenUsed)})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("dashboard target row mismatch")
			}
		}
		return nil
	})
	if err != nil {
		releaseFlushErr = errors.New("dashboard commit failed or uncertain; reconciliation required")
		common.ReleaseWriteFailed()
		return releaseFlushErr
	}
	CacheQuotaData = make(map[string]*QuotaData)
	return nil
}

func ReleasePending() (int, int, bool) {
	releaseFlushMu.Lock()
	defer releaseFlushMu.Unlock()
	batch := 0
	for i := range batchUpdateStores {
		batchUpdateLocks[i].Lock()
		batch += len(batchUpdateStores[i])
		batchUpdateLocks[i].Unlock()
	}
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	return batch, len(CacheQuotaData), releaseFlushErr != nil
}
