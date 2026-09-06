package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// RecoveryCandidates preserves group/model authorization in both cache modes.
func RecoveryCandidates(group, modelName string) ([]*Channel, error) {
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		ids := group2model2channels[group][modelName]
		if len(ids) == 0 {
			ids = group2model2channels[group][ratio_setting.FormatMatchingModelName(modelName)]
		}
		result := make([]*Channel, 0, len(ids))
		for _, id := range ids {
			if ch := channelsIDM[id]; ch != nil && ch.Status == common.ChannelStatusEnabled {
				result = append(result, ch)
			}
		}
		return result, nil
	}
	var ids []int
	query := func(name string) error {
		return DB.Model(&Ability{}).Where(commonGroupCol+" = ? AND model = ? AND enabled = ?", group, name, true).Pluck("channel_id", &ids).Error
	}
	if err := query(modelName); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := query(ratio_setting.FormatMatchingModelName(modelName)); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var result []*Channel
	err := DB.Where("id IN ? AND status = ?", ids, common.ChannelStatusEnabled).Find(&result).Error
	return result, err
}
