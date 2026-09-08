package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestEffectiveChannelWeightUsesRecentRuntimeHealth(t *testing.T) {
	resetChannelRuntimeScores()
	setting := operation_setting.GetMonitorSetting()
	original := *setting
	t.Cleanup(func() { *setting = original; resetChannelRuntimeScores() })
	setting.DynamicChannelWeightEnabled = true
	setting.DynamicChannelWeightMinSamples = 2
	setting.DynamicChannelWeightWindowMinutes = 15
	setting.DynamicChannelWeightMinMultiplier = 0.25
	setting.DynamicChannelWeightMaxMultiplier = 2
	weight := uint(100)
	ch := &model.Channel{Id: 17, Weight: &weight}
	ObserveChannelRuntimeResult("g", "m", ch.Id, time.Second, 200, true)
	ObserveChannelRuntimeResult("g", "m", ch.Id, 30*time.Second, 503, false)
	require.Less(t, EffectiveChannelWeight(ch, "g", "m"), 100)
}

func TestEffectiveChannelWeightKeepsBaseUntilEnoughSamples(t *testing.T) {
	resetChannelRuntimeScores()
	setting := operation_setting.GetMonitorSetting()
	original := *setting
	t.Cleanup(func() { *setting = original; resetChannelRuntimeScores() })
	setting.DynamicChannelWeightEnabled = true
	setting.DynamicChannelWeightMinSamples = 3
	weight := uint(40)
	ch := &model.Channel{Id: 18, Weight: &weight}
	ObserveChannelRuntimeResult("g", "m", ch.Id, time.Second, 200, true)
	require.Equal(t, 40, EffectiveChannelWeight(ch, "g", "m"))
}

func TestCalculateChannelRuntimeScoreExposesDecisionEvidence(t *testing.T) {
	resetChannelRuntimeScores()
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	setting := operation_setting.GetMonitorSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
		common.RedisEnabled = oldRedis
		resetChannelRuntimeScores()
	})
	setting.DynamicChannelWeightEnabled = true
	setting.DynamicChannelWeightMinSamples = 2
	setting.DynamicChannelWeightWindowMinutes = 15
	setting.DynamicChannelWeightTargetFRTMs = 5000
	setting.DynamicChannelWeightErrorPenalty = 1
	setting.DynamicChannelWeight429Penalty = 1.5
	setting.DynamicChannelWeightMinMultiplier = 0.25
	setting.DynamicChannelWeightMaxMultiplier = 2

	weight := uint(100)
	ch := &model.Channel{Id: 19, Weight: &weight}
	ObserveChannelRuntimeResult("g", "m", ch.Id, 2*time.Second, 200, true)
	ObserveChannelRuntimeResult("g", "m", ch.Id, 8*time.Second, 429, false)

	score := CalculateChannelRuntimeScore(ch, "g", "m")
	require.True(t, score.Ready)
	require.Equal(t, 2, score.SampleCount)
	require.Equal(t, int64(8000), score.MedianFRTMs)
	require.InDelta(t, 0.5, score.SuccessRate, 0.0001)
	require.InDelta(t, 0.5, score.Rate429, 0.0001)
	require.Zero(t, score.Rate5xx)
	require.Less(t, score.EffectiveWeight, score.BaseWeight)
	require.Equal(t, "memory", score.Source)
}
