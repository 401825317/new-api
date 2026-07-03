package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPromptCacheSummaryLogDB(t *testing.T) {
	t.Helper()

	originalLogDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalLogSqlType := common.LogSqlType

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.LogSqlType = common.DatabaseTypeSQLite
	initCol()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db

	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.LogSqlType = originalLogSqlType
		initCol()
	})
}

func TestGetPromptCacheUsageSummaryAggregatesConsumeLogs(t *testing.T) {
	setupPromptCacheSummaryLogDB(t)
	now := time.Now().Unix()

	logs := []Log{
		{
			CreatedAt:        now - 30,
			Type:             LogTypeConsume,
			Username:         "alice",
			ModelName:        "smart-latest",
			ChannelId:        9,
			PromptTokens:     1000,
			CompletionTokens: 20,
			Other:            common.MapToJsonStr(map[string]interface{}{"cache_tokens": 400}),
		},
		{
			CreatedAt:        now - 20,
			Type:             LogTypeConsume,
			Username:         "alice",
			ModelName:        "smart-latest",
			ChannelId:        9,
			PromptTokens:     500,
			CompletionTokens: 10,
			Other:            common.MapToJsonStr(map[string]interface{}{"cache_tokens": 0}),
		},
		{
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Username:         "bob",
			ModelName:        "image",
			ChannelId:        11,
			PromptTokens:     200,
			CompletionTokens: 5,
			Other:            common.MapToJsonStr(map[string]interface{}{"cache_tokens": "100"}),
		},
		{
			CreatedAt:    now - 10,
			Type:         LogTypeError,
			Username:     "alice",
			ModelName:    "smart-latest",
			ChannelId:    9,
			PromptTokens: 999,
			Other:        common.MapToJsonStr(map[string]interface{}{"cache_tokens": 999}),
		},
		{
			CreatedAt:    now - 7200,
			Type:         LogTypeConsume,
			Username:     "alice",
			ModelName:    "smart-latest",
			ChannelId:    9,
			PromptTokens: 1000,
			Other:        common.MapToJsonStr(map[string]interface{}{"cache_tokens": 1000}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	summary, err := GetPromptCacheUsageSummary(PromptCacheUsageSummaryFilter{
		StartTimestamp: now - 3600,
		EndTimestamp:   now + 1,
		Limit:          10,
	})
	require.NoError(t, err)

	require.EqualValues(t, 3, summary.TotalRequests)
	require.EqualValues(t, 2, summary.HitRequests)
	require.InDelta(t, 2.0/3.0, summary.RequestHitRate, 0.0001)
	require.EqualValues(t, 1700, summary.PromptTokens)
	require.EqualValues(t, 35, summary.CompletionTokens)
	require.EqualValues(t, 1735, summary.TotalTokens)
	require.EqualValues(t, 500, summary.CachedTokens)
	require.True(t, summary.TokenCacheRateAvailable)
	require.InDelta(t, 500.0/1700.0, summary.TokenCacheRate, 0.0001)
	require.Len(t, summary.ByModel, 2)
	require.Equal(t, "smart-latest", summary.ByModel[0].ModelName)
	require.EqualValues(t, 2, summary.ByModel[0].TotalRequests)
	require.EqualValues(t, 1, summary.ByModel[0].HitRequests)
	require.Len(t, summary.ByChannel, 2)
	require.EqualValues(t, 9, summary.ByChannel[0].ChannelID)
}

func TestGetPromptCacheUsageSummaryFiltersByUsername(t *testing.T) {
	setupPromptCacheSummaryLogDB(t)
	now := time.Now().Unix()

	logs := []Log{
		{
			CreatedAt:    now - 30,
			Type:         LogTypeConsume,
			Username:     "alice",
			ModelName:    "smart-latest",
			ChannelId:    9,
			PromptTokens: 1000,
			Other:        common.MapToJsonStr(map[string]interface{}{"cache_tokens": 400}),
		},
		{
			CreatedAt:    now - 10,
			Type:         LogTypeConsume,
			Username:     "bob",
			ModelName:    "smart-latest",
			ChannelId:    9,
			PromptTokens: 1000,
			Other:        common.MapToJsonStr(map[string]interface{}{"cache_tokens": 0}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	summary, err := GetPromptCacheUsageSummary(PromptCacheUsageSummaryFilter{
		StartTimestamp: now - 3600,
		EndTimestamp:   now + 1,
		Username:       "alice",
	})
	require.NoError(t, err)

	require.EqualValues(t, 1, summary.TotalRequests)
	require.EqualValues(t, 1, summary.HitRequests)
	require.EqualValues(t, 400, summary.CachedTokens)
	require.InDelta(t, 0.4, summary.TokenCacheRate, 0.0001)
}
