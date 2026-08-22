package service

import (
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var channelAffinityUsageCacheTestSequence atomic.Uint64

func uniqueChannelAffinityUsageCacheTestKey(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("%s:%s:%d", t.Name(), suffix, channelAffinityUsageCacheTestSequence.Add(1))
}

func buildChannelAffinityStatsContextForTest(ruleName, usingGroup, keyFP string) *gin.Context {
	return buildChannelAffinityStatsContextWithModelForTest(ruleName, usingGroup, keyFP, "", 0)
}

func buildChannelAffinityStatsContextWithModelForTest(ruleName, usingGroup, keyFP, modelName string, channelID int) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	setChannelAffinityContext(ctx, channelAffinityMeta{
		CacheKey:       fmt.Sprintf("test:%s:%s:%s", ruleName, usingGroup, keyFP),
		TTLSeconds:     600,
		RuleName:       ruleName,
		UsingGroup:     usingGroup,
		KeyFingerprint: keyFP,
		ModelName:      modelName,
	})
	if channelID > 0 {
		ctx.Set("channel_id", channelID)
	}
	return ctx
}

func TestObserveChannelAffinityUsageCacheByRelayFormat_ClaudeMode(t *testing.T) {
	ruleName := uniqueChannelAffinityUsageCacheTestKey(t, "rule")
	usingGroup := "default"
	keyFP := uniqueChannelAffinityUsageCacheTestKey(t, "claude")
	ctx := buildChannelAffinityStatsContextForTest(ruleName, usingGroup, keyFP)

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 40,
		TotalTokens:      140,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 30,
		},
	}

	ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, types.RelayFormatClaude)
	stats := GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFP)

	require.EqualValues(t, 1, stats.Total)
	require.EqualValues(t, 1, stats.Hit)
	require.EqualValues(t, 100, stats.PromptTokens)
	require.EqualValues(t, 40, stats.CompletionTokens)
	require.EqualValues(t, 140, stats.TotalTokens)
	require.EqualValues(t, 30, stats.CachedTokens)
	require.Equal(t, cacheTokenRateModeCachedOverPromptPlusCached, stats.CachedTokenRateMode)
}

func TestObserveChannelAffinityUsageCacheByRelayFormat_MixedMode(t *testing.T) {
	ruleName := uniqueChannelAffinityUsageCacheTestKey(t, "rule")
	usingGroup := "default"
	keyFP := uniqueChannelAffinityUsageCacheTestKey(t, "mixed")
	ctx := buildChannelAffinityStatsContextForTest(ruleName, usingGroup, keyFP)

	openAIUsage := &dto.Usage{
		PromptTokens: 100,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10,
		},
	}
	claudeUsage := &dto.Usage{
		PromptTokens: 80,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 20,
		},
	}

	ObserveChannelAffinityUsageCacheByRelayFormat(ctx, openAIUsage, types.RelayFormatOpenAI)
	ObserveChannelAffinityUsageCacheByRelayFormat(ctx, claudeUsage, types.RelayFormatClaude)
	stats := GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFP)

	require.EqualValues(t, 2, stats.Total)
	require.EqualValues(t, 2, stats.Hit)
	require.EqualValues(t, 180, stats.PromptTokens)
	require.EqualValues(t, 30, stats.CachedTokens)
	require.Equal(t, cacheTokenRateModeMixed, stats.CachedTokenRateMode)
}

func TestObserveChannelAffinityUsageCacheByRelayFormat_UnsupportedModeKeepsEmpty(t *testing.T) {
	ruleName := uniqueChannelAffinityUsageCacheTestKey(t, "rule")
	usingGroup := "default"
	keyFP := uniqueChannelAffinityUsageCacheTestKey(t, "unsupported")
	ctx := buildChannelAffinityStatsContextForTest(ruleName, usingGroup, keyFP)

	usage := &dto.Usage{
		PromptTokens: 100,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 25,
		},
	}

	ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, types.RelayFormatGemini)
	stats := GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFP)

	require.EqualValues(t, 1, stats.Total)
	require.EqualValues(t, 1, stats.Hit)
	require.EqualValues(t, 25, stats.CachedTokens)
	require.Equal(t, "", stats.CachedTokenRateMode)
}

func TestGetChannelAffinityUsageCacheSummaryWithFilter_ByModel(t *testing.T) {
	ruleName := uniqueChannelAffinityUsageCacheTestKey(t, "summary-rule")
	usingGroup := "default"
	modelName := uniqueChannelAffinityUsageCacheTestKey(t, "gpt-summary")
	otherModelName := uniqueChannelAffinityUsageCacheTestKey(t, "image-summary")

	ctx := buildChannelAffinityStatsContextWithModelForTest(ruleName, usingGroup, "fp_model_1", modelName, 7)
	otherCtx := buildChannelAffinityStatsContextWithModelForTest(ruleName, usingGroup, "fp_model_2", otherModelName, 8)

	ObserveChannelAffinityUsageCacheByRelayFormat(ctx, &dto.Usage{
		PromptTokens: 100,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 20,
		},
	}, types.RelayFormatOpenAI)
	ObserveChannelAffinityUsageCacheByRelayFormat(otherCtx, &dto.Usage{
		PromptTokens: 100,
	}, types.RelayFormatOpenAI)

	summary := GetChannelAffinityUsageCacheSummaryWithFilter(ChannelAffinityUsageCacheSummaryFilter{
		RuleName: ruleName,
		ModelNames: map[string]struct{}{
			modelName: {},
		},
	})

	require.EqualValues(t, 1, summary.Total)
	require.EqualValues(t, 1, summary.Hit)
	require.Len(t, summary.ByModel, 1)
	require.Equal(t, modelName, summary.ByModel[0].ModelName)
	require.EqualValues(t, 1, summary.ByModel[0].Total)
	require.EqualValues(t, 1, summary.ByModel[0].Hit)
	require.True(t, summary.ByModel[0].TokenCacheRateAvailable)
	require.InDelta(t, 0.2, summary.ByModel[0].TokenCacheRate, 0.0001)
	require.Len(t, summary.ByChannel, 1)
	require.EqualValues(t, 7, summary.ByChannel[0].ChannelID)
}
