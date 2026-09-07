package service

import (
	"testing"
	"time"

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
