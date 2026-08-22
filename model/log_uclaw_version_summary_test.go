package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uclawSummaryOther(
	version string,
	commit string,
	buildId string,
	latencyMs float64,
) string {
	return common.MapToJsonStr(map[string]interface{}{
		"end_to_end_upstream_response_ms": latencyMs,
		"client_diagnostics": map[string]interface{}{
			"uclaw_client":   "desktop",
			"uclaw_version":  version,
			"uclaw_commit":   commit,
			"uclaw_build_id": buildId,
			"uclaw_platform": "win32",
			"uclaw_arch":     "x64",
			"uclaw_channel":  "stable",
			"uclaw_mode":     "portable",
		},
	})
}

func TestRecordConsumeLogKeepsAnonymousUClawSuccessMetricWhenDetailedLogsDisabled(t *testing.T) {
	setupPromptCacheSummaryLogDB(t)
	previous := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previous })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", strings.NewReader("private prompt"))
	c.Request.Header.Set("X-UClaw-Client", "desktop")
	c.Request.Header.Set("X-UClaw-Version", "2.0.3")
	c.Request.Header.Set("X-UClaw-Commit", "abc123")
	c.Request.Header.Set("X-UClaw-Build-Id", "build-a")
	c.Request.Header.Set("X-Request-Id", "must-not-be-persisted")
	c.Set("clawx_device_id", "device-test")
	c.Set("clawx_session_id", "session-test")
	c.Set("username", "must-not-be-persisted")

	RecordConsumeLog(c, 42, RecordConsumeLogParams{
		ChannelId:        9,
		PromptTokens:     100,
		CompletionTokens: 50,
		ModelName:        "private-model",
		TokenName:        "private-token-name",
		Quota:            123,
		Content:          "private prompt and response",
		TokenId:          88,
		UseTimeSeconds:   7,
		Group:            "private-group",
		Other: map[string]interface{}{
			"prompt": "private prompt",
		},
	})

	var stored Log
	require.NoError(t, LOG_DB.First(&stored).Error)
	assert.Equal(t, LogTypeConsume, stored.Type)
	assert.Equal(t, 7, stored.UseTime)
	assert.Zero(t, stored.UserId)
	assert.Empty(t, stored.Username)
	assert.Empty(t, stored.Content)
	assert.Empty(t, stored.ModelName)
	assert.Empty(t, stored.TokenName)
	assert.Zero(t, stored.TokenId)
	assert.Zero(t, stored.PromptTokens)
	assert.Zero(t, stored.CompletionTokens)
	assert.NotContains(t, stored.Other, "must-not-be-persisted")
	assert.NotContains(t, stored.Other, "private")

	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(stored.Other, &other))
	assert.Equal(t, map[string]interface{}{
		"uclaw_client":   "desktop",
		"uclaw_version":  "2.0.3",
		"uclaw_commit":   "abc123",
		"uclaw_build_id": "build-a",
	}, other["client_diagnostics"])
}

func TestGetUClawVersionUsageSummaryAggregatesVersionHealth(t *testing.T) {
	setupPromptCacheSummaryLogDB(t)
	now := time.Now().Unix()
	logs := []Log{
		{CreatedAt: now - 5, Type: LogTypeConsume, UseTime: 1, Other: uclawSummaryOther("2.0.3", "abc123", "build-a", 100)},
		{CreatedAt: now - 4, Type: LogTypeConsume, UseTime: 1, Other: uclawSummaryOther("2.0.3", "abc123", "build-a", 300)},
		{CreatedAt: now - 3, Type: LogTypeError, UseTime: 1, Other: uclawSummaryOther("2.0.3", "abc123", "build-a", 500)},
		{CreatedAt: now - 2, Type: LogTypeConsume, UseTime: 2, Other: uclawSummaryOther("2.0.2", "def456", "build-b", 0)},
		{CreatedAt: now - 1, Type: LogTypeConsume, Other: common.MapToJsonStr(map[string]interface{}{"client_diagnostics": map[string]interface{}{"user_agent": "other"}})},
		{CreatedAt: now - 1, Type: LogTypeTopup, Other: uclawSummaryOther("2.0.3", "abc123", "build-a", 50)},
		{CreatedAt: now - 3600, Type: LogTypeConsume, Other: uclawSummaryOther("2.0.3", "abc123", "build-a", 900)},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	summary, err := GetUClawVersionUsageSummary(UClawVersionUsageFilter{
		StartTimestamp: now - 60,
		EndTimestamp:   now + 1,
	})
	require.NoError(t, err)
	require.Len(t, summary.Items, 2)
	assert.EqualValues(t, 4, summary.TotalRequests)
	assert.False(t, summary.Truncated)

	current := summary.Items[0]
	assert.Equal(t, "2.0.3", current.Version)
	assert.Equal(t, "abc123", current.Commit)
	assert.Equal(t, "build-a", current.BuildId)
	assert.Equal(t, "win32", current.Platform)
	assert.Equal(t, "x64", current.Arch)
	assert.Equal(t, "stable", current.Channel)
	assert.Equal(t, "portable", current.Mode)
	assert.EqualValues(t, 3, current.RequestCount)
	assert.EqualValues(t, 2, current.SuccessCount)
	assert.EqualValues(t, 1, current.ErrorCount)
	assert.InDelta(t, 2.0/3.0, current.SuccessRate, 0.0001)
	assert.InDelta(t, 1.0/3.0, current.ErrorRate, 0.0001)
	assert.InDelta(t, 300, current.AverageLatencyMs, 0.0001)
	assert.InDelta(t, 500, current.P95LatencyMs, 0.0001)

	previous := summary.Items[1]
	assert.Equal(t, "2.0.2", previous.Version)
	assert.EqualValues(t, 1, previous.RequestCount)
	assert.InDelta(t, 2000, previous.AverageLatencyMs, 0.0001)
	assert.InDelta(t, 2000, previous.P95LatencyMs, 0.0001)
}

func TestGetUClawVersionUsageSummaryExcludesNonUClawAndUnsafeIdentity(t *testing.T) {
	setupPromptCacheSummaryLogDB(t)
	now := time.Now().Unix()
	logs := []Log{
		{
			CreatedAt: now - 3,
			Type:      LogTypeConsume,
			Other: common.MapToJsonStr(map[string]interface{}{
				"client_diagnostics": map[string]interface{}{
					"uclaw_version": "2.0.3",
				},
			}),
		},
		{
			CreatedAt: now - 2,
			Type:      LogTypeConsume,
			Other: common.MapToJsonStr(map[string]interface{}{
				"client_diagnostics": map[string]interface{}{
					"uclaw_client":  "desktop",
					"uclaw_version": `C:\Users\Alice\private.txt`,
				},
			}),
		},
		{
			CreatedAt: now - 1,
			Type:      LogTypeConsume,
			Other:     uclawSummaryOther("2.0.3", "abc123", "build-a", 125),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	summary, err := GetUClawVersionUsageSummary(UClawVersionUsageFilter{
		StartTimestamp: now - 60,
		EndTimestamp:   now + 1,
	})
	require.NoError(t, err)
	require.Len(t, summary.Items, 1)
	assert.EqualValues(t, 1, summary.TotalRequests)
	assert.Equal(t, "2.0.3", summary.Items[0].Version)
}
