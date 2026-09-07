package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// channelRuntimeSample is intentionally small: the scorer only needs a recent
// FRT and outcome, not request content or any credential-related data.
type channelRuntimeSample struct {
	At         int64 `json:"at"`
	FRTMs      int64 `json:"frt_ms"`
	Success    bool  `json:"success"`
	StatusCode int   `json:"status_code"`
}

var channelRuntimeScores = struct {
	sync.RWMutex
	items map[string][]channelRuntimeSample
}{items: make(map[string][]channelRuntimeSample)}

func channelRuntimeKey(group, modelName string, channelID int) string {
	return group + "\x00" + modelName + "\x00" + strconv.Itoa(channelID)
}

func channelRuntimeRedisKey(group, modelName string, channelID int) string {
	sum := sha256.Sum256([]byte(channelRuntimeKey(group, modelName, channelID)))
	return "new-api:channel-runtime-score:v1:" + hex.EncodeToString(sum[:])
}

// ObserveChannelRuntimeResult records one completed or failed upstream attempt.
// Redis is the shared source when available; the process-local store remains a
// fallback for single-instance or Redis-degraded operation.
func ObserveChannelRuntimeResult(group, modelName string, channelID int, frt time.Duration, statusCode int, success bool) {
	if !operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled || channelID <= 0 {
		return
	}
	now := time.Now()
	key := channelRuntimeKey(group, modelName, channelID)
	sample := channelRuntimeSample{At: now.UnixMilli(), FRTMs: frt.Milliseconds(), Success: success, StatusCode: statusCode}
	channelRuntimeScores.Lock()
	window := time.Duration(operation_setting.GetMonitorSetting().DynamicChannelWeightWindowMinutes) * time.Minute
	if window <= 0 {
		window = 15 * time.Minute
	}
	samples := append(channelRuntimeScores.items[key], sample)
	cutoff := now.Add(-window).UnixMilli()
	first := 0
	for first < len(samples) && samples[first].At < cutoff {
		first++
	}
	if first > 0 {
		samples = samples[first:]
	}
	if len(samples) > 2000 {
		samples = samples[len(samples)-2000:]
	}
	channelRuntimeScores.items[key] = samples
	channelRuntimeScores.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		payload, err := common.Marshal(sample)
		if err == nil {
			redisKey := channelRuntimeRedisKey(group, modelName, channelID)
			ctx := context.Background()
			pipe := common.RDB.TxPipeline()
			pipe.RPush(ctx, redisKey, string(payload))
			pipe.LTrim(ctx, redisKey, -2000, -1)
			pipe.Expire(ctx, redisKey, window+time.Minute)
			if _, err = pipe.Exec(ctx); err != nil {
				common.SysError("dynamic channel weight redis write failed")
			}
		}
	}
}

func loadChannelRuntimeSamples(group, modelName string, channelID int, cutoff int64) []channelRuntimeSample {
	if common.RedisEnabled && common.RDB != nil {
		values, err := common.RDB.LRange(context.Background(), channelRuntimeRedisKey(group, modelName, channelID), 0, -1).Result()
		if err == nil {
			samples := make([]channelRuntimeSample, 0, len(values))
			for _, value := range values {
				var sample channelRuntimeSample
				if common.UnmarshalJsonStr(value, &sample) == nil && sample.At >= cutoff {
					samples = append(samples, sample)
				}
			}
			if len(samples) > 0 {
				return samples
			}
		}
	}
	channelRuntimeScores.RLock()
	defer channelRuntimeScores.RUnlock()
	local := channelRuntimeScores.items[channelRuntimeKey(group, modelName, channelID)]
	samples := make([]channelRuntimeSample, 0, len(local))
	for _, sample := range local {
		if sample.At >= cutoff {
			samples = append(samples, sample)
		}
	}
	return samples
}

// EffectiveChannelWeight returns the configured weight adjusted by recent
// runtime health. Priority is deliberately ignored here: callers retain the
// existing priority tiers and only use this value within one tier.
func EffectiveChannelWeight(ch *model.Channel, group, modelName string) int {
	if ch == nil {
		return 0
	}
	base := ch.GetWeight()
	if !operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled {
		return base
	}
	setting := operation_setting.GetMonitorSetting()
	window := time.Duration(setting.DynamicChannelWeightWindowMinutes) * time.Minute
	if window <= 0 {
		window = 15 * time.Minute
	}
	cutoff := time.Now().Add(-window)
	samples := loadChannelRuntimeSamples(group, modelName, ch.Id, cutoff.UnixMilli())
	var count, successCount, throttledCount, serverErrorCount int
	frts := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample.At < cutoff.UnixMilli() {
			continue
		}
		count++
		if sample.Success {
			successCount++
		}
		if sample.FRTMs > 0 {
			frts = append(frts, time.Duration(sample.FRTMs)*time.Millisecond)
		}
		if sample.StatusCode == 429 {
			throttledCount++
		}
		if sample.StatusCode >= 500 {
			serverErrorCount++
		}
	}
	minSamples := setting.DynamicChannelWeightMinSamples
	if minSamples < 1 || count < minSamples {
		return base
	}

	successRate := float64(successCount) / float64(count)
	frtScore := 1.0
	if len(frts) > 0 {
		sort.Slice(frts, func(i, j int) bool { return frts[i] < frts[j] })
		medianFRT := frts[len(frts)/2]
		targetFRT := time.Duration(setting.DynamicChannelWeightTargetFRTMs) * time.Millisecond
		if targetFRT <= 0 {
			targetFRT = 8 * time.Second
		}
		frtScore = float64(targetFRT) / float64(medianFRT)
		frtScore = math.Max(0.5, math.Min(1.5, frtScore))
	}
	errorPenalty := math.Max(0, setting.DynamicChannelWeightErrorPenalty)
	throttlePenalty := math.Max(0, setting.DynamicChannelWeight429Penalty)
	healthScore := successRate - errorPenalty*float64(serverErrorCount)/float64(count) - throttlePenalty*float64(throttledCount)/float64(count)
	healthScore = math.Max(0, math.Min(1, healthScore))
	score := 0.7*healthScore + 0.3*frtScore
	minMultiplier := setting.DynamicChannelWeightMinMultiplier
	maxMultiplier := setting.DynamicChannelWeightMaxMultiplier
	if minMultiplier <= 0 {
		minMultiplier = 0.25
	}
	if maxMultiplier < minMultiplier {
		maxMultiplier = 2
	}
	if score < minMultiplier {
		score = minMultiplier
	}
	if score > maxMultiplier {
		score = maxMultiplier
	}
	if base == 0 {
		base = 10
	}
	weight := int(math.Round(float64(base) * score))
	if weight < 1 {
		weight = 1
	}
	return weight
}

// resetChannelRuntimeScores is test-only support kept private to avoid making
// the runtime score store part of the public API.
func resetChannelRuntimeScores() {
	channelRuntimeScores.Lock()
	channelRuntimeScores.items = make(map[string][]channelRuntimeSample)
	channelRuntimeScores.Unlock()
}
