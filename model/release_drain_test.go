package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"sync"
	"testing"
)

type uncertainCommitPool struct{ gorm.ConnPool }

func (p uncertainCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(gorm.TxBeginner).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &uncertainCommitTx{tx}, nil
}

type uncertainCommitTx struct{ *sql.Tx }

func (tx uncertainCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errors.New("connection lost after commit")
}

func TestReleaseDrainAmbiguousCommitDoesNotDoubleCharge(t *testing.T) {
	db := releaseTestDB(t)
	db.Statement.ConnPool = uncertainCommitPool{db.Statement.ConnPool}
	addNewRecord(BatchUpdateTypeUserQuota, 1, -25)
	if err := FlushReleaseBatch(); err == nil {
		t.Fatal("uncertain commit accepted")
	}
	var user User
	db.First(&user, 1)
	if user.Quota != 975 {
		t.Fatal("test did not commit before error")
	}
	if err := FlushReleaseBatch(); err == nil {
		t.Fatal("uncertain commit replayed")
	}
	db.First(&user, 1)
	if user.Quota != 975 {
		t.Fatal("double charged")
	}
	batch, _, failed := ReleasePending()
	if batch != 1 || !failed {
		t.Fatal("uncertain batch lost")
	}
}

func TestReleaseDrainAllBatchFields(t *testing.T) {
	db := releaseTestDB(t)
	if err := db.Create(&Token{Id: 1, Key: "release-test", UserId: 1, RemainQuota: 1000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Channel{Id: 1, Name: "release-test"}).Error; err != nil {
		t.Fatal(err)
	}
	addNewRecord(BatchUpdateTypeUserQuota, 1, -25)
	addNewRecord(BatchUpdateTypeTokenQuota, 1, -25)
	addNewRecord(BatchUpdateTypeUsedQuota, 1, 25)
	addNewRecord(BatchUpdateTypeChannelUsedQuota, 1, 25)
	addNewRecord(BatchUpdateTypeRequestCount, 1, 1)
	if err := FlushReleaseBatch(); err != nil {
		t.Fatal(err)
	}
	var user User
	var token Token
	var channel Channel
	db.First(&user, 1)
	db.First(&token, 1)
	db.First(&channel, 1)
	if user.Quota != 975 || user.UsedQuota != 25 || user.RequestCount != 1 || token.RemainQuota != 975 || token.UsedQuota != 25 || channel.UsedQuota != 25 {
		t.Fatal("batch field mapping changed")
	}
}

func releaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldLog := DB, LOG_DB
	oldEnabled, oldTracker := common.ReleaseDrainEnabled, common.ReleaseDrain
	oldStores, oldCache, oldErr := batchUpdateStores, CacheQuotaData, releaseFlushErr
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&User{}, &Token{}, &Channel{}, &QuotaData{}); err != nil {
		t.Fatal(err)
	}
	DB, LOG_DB = db, db
	common.ReleaseDrainEnabled = true
	common.ReleaseDrain = &common.DrainTracker{}
	batchUpdateStores = make([]map[int]int, BatchUpdateTypeCount)
	for i := range batchUpdateStores {
		batchUpdateStores[i] = map[int]int{}
	}
	CacheQuotaData = map[string]*QuotaData{}
	releaseFlushErr = nil
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLog
		common.ReleaseDrainEnabled, common.ReleaseDrain = oldEnabled, oldTracker
		batchUpdateStores, CacheQuotaData, releaseFlushErr = oldStores, oldCache, oldErr
		sqlDB.Close()
	})
	if err := db.Create(&User{Id: 1, Username: "drain-test", Quota: 1000}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReleaseDrainBatchRollbackAndNoReplay(t *testing.T) {
	db := releaseTestDB(t)
	addNewRecord(BatchUpdateTypeUserQuota, 1, -25)
	addNewRecord(BatchUpdateTypeTokenQuota, 999, -25)
	if err := FlushReleaseBatch(); err == nil {
		t.Fatal("missing token accepted")
	}
	var user User
	db.First(&user, 1)
	if user.Quota != 1000 {
		t.Fatal("partial batch committed")
	}
	batch, _, failed := ReleasePending()
	if batch != 2 || !failed {
		t.Fatal("failed batch discarded")
	}
	if err := db.Create(&Token{Id: 999, Key: "release-test", UserId: 1, RemainQuota: 1000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := FlushReleaseBatch(); err == nil {
		t.Fatal("failed/uncertain batch automatically replayed")
	}
	db.First(&user, 1)
	if user.Quota != 1000 {
		t.Fatal("replay changed quota")
	}
}

func TestReleaseDrainBatchConcurrentExactlyOnce(t *testing.T) {
	db := releaseTestDB(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			addNewRecord(BatchUpdateTypeUserQuota, 1, -1)
			if err := FlushReleaseBatch(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := FlushReleaseBatch(); err != nil {
		t.Fatal(err)
	}
	var user User
	db.First(&user, 1)
	if user.Quota != 950 {
		t.Fatalf("quota=%d", user.Quota)
	}
	batch, _, failed := ReleasePending()
	if batch != 0 || failed {
		t.Fatal("batch not empty")
	}
}

func TestReleaseDrainDashboardRollback(t *testing.T) {
	db := releaseTestDB(t)
	for i := 0; i < 2; i++ {
		LogQuotaData(1, "drain-test", fmt.Sprint(i), 20, 3600, 5)
	}
	writes := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_data" {
			writes++
			if writes == 2 {
				tx.AddError(errors.New("injected write failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := FlushReleaseQuotaData(); err == nil {
		t.Fatal("failure ignored")
	}
	var count int64
	db.Model(&QuotaData{}).Count(&count)
	if count != 0 {
		t.Fatal("partial dashboard committed")
	}
	_, pending, failed := ReleasePending()
	if pending != 2 || !failed {
		t.Fatal("dashboard cache lost")
	}
	db.Callback().Create().Remove("test:fail")
	if err := FlushReleaseQuotaData(); err == nil {
		t.Fatal("uncertain dashboard replayed")
	}
}

func TestReleaseDrainDashboardExactlyOnce(t *testing.T) {
	db := releaseTestDB(t)
	LogQuotaData(1, "drain-test", "model", 20, 3600, 5)
	for i := 0; i < 2; i++ {
		if err := FlushReleaseQuotaData(); err != nil {
			t.Fatal(err)
		}
	}
	LogQuotaData(1, "drain-test", "model", 30, 3600, 7)
	if err := FlushReleaseQuotaData(); err != nil {
		t.Fatal(err)
	}
	var rows []QuotaData
	db.Find(&rows)
	if len(rows) != 1 || rows[0].Quota != 50 || rows[0].Count != 2 || rows[0].TokenUsed != 12 {
		t.Fatalf("bad dashboard: %+v", rows)
	}
}

func TestReleaseDrainWriteGuard(t *testing.T) {
	db := releaseTestDB(t)
	if err := InstallReleaseWriteGuards(); err != nil {
		t.Fatal(err)
	}
	db.Exec("UPDATE nonexistent_table SET value=1")
	if !common.ReleaseDrain.Snapshot().Failed {
		t.Fatal("write failure not latched")
	}
}
