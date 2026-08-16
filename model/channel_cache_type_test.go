package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannelByType(t *testing.T) {
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGroup2Model2Channels := group2model2channels
	oldChannelsIDM := channelsIDM
	oldAdvancedCustomConfig := channel2advancedCustomConfig

	priorityGPT := int64(100)
	priorityDeepSeek := int64(0)
	weight := uint(1)
	channelSyncLock.Lock()
	common.MemoryCacheEnabled = true
	group2model2channels = map[string]map[string][]int{
		"default": {"smart-latest": {9, 12}},
	}
	channelsIDM = map[int]*Channel{
		9:  {Id: 9, Type: constant.ChannelTypeOpenAI, Priority: &priorityGPT, Weight: &weight},
		12: {Id: 12, Type: constant.ChannelTypeDeepSeek, Priority: &priorityDeepSeek, Weight: &weight},
	}
	channel2advancedCustomConfig = map[int]*dto.AdvancedCustomConfig{}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		group2model2channels = oldGroup2Model2Channels
		channelsIDM = oldChannelsIDM
		channel2advancedCustomConfig = oldAdvancedCustomConfig
		channelSyncLock.Unlock()
	})

	channel, err := GetRandomSatisfiedChannelByType(
		"default",
		"smart-latest",
		0,
		"/v1/responses",
		constant.ChannelTypeDeepSeek,
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 12, channel.Id)
}
