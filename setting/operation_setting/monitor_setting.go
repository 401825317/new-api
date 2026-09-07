package operation_setting

import (
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled            bool    `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes            float64 `json:"auto_test_channel_minutes"`
	DynamicChannelWeightEnabled       bool    `json:"dynamic_channel_weight_enabled"`
	DynamicChannelWeightWindowMinutes int     `json:"dynamic_channel_weight_window_minutes"`
	DynamicChannelWeightMinSamples    int     `json:"dynamic_channel_weight_min_samples"`
	DynamicChannelWeightTargetFRTMs   int     `json:"dynamic_channel_weight_target_frt_ms"`
	DynamicChannelWeightErrorPenalty  float64 `json:"dynamic_channel_weight_error_penalty"`
	DynamicChannelWeight429Penalty    float64 `json:"dynamic_channel_weight_429_penalty"`
	DynamicChannelWeightMinMultiplier float64 `json:"dynamic_channel_weight_min_multiplier"`
	DynamicChannelWeightMaxMultiplier float64 `json:"dynamic_channel_weight_max_multiplier"`
}

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled:            false,
	AutoTestChannelMinutes:            10,
	DynamicChannelWeightEnabled:       false,
	DynamicChannelWeightWindowMinutes: 15,
	DynamicChannelWeightMinSamples:    20,
	DynamicChannelWeightTargetFRTMs:   8000,
	DynamicChannelWeightErrorPenalty:  1,
	DynamicChannelWeight429Penalty:    1.5,
	DynamicChannelWeightMinMultiplier: 0.25,
	DynamicChannelWeightMaxMultiplier: 2,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

func GetMonitorSetting() *MonitorSetting {
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err == nil && frequency > 0 {
			monitorSetting.AutoTestChannelEnabled = true
			monitorSetting.AutoTestChannelMinutes = float64(frequency)
		}
	}
	return &monitorSetting
}
