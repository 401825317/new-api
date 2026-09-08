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

// ChannelRuntimeScore is the exact snapshot used for one weighted selection.
// It is safe to expose in administrator-only log metadata: it contains no
// request content, credential, account name, or upstream URL.
type ChannelRuntimeScore struct {
	ChannelID       int     `json:"channel_id"`
	BaseWeight      int     `json:"base_weight"`
	EffectiveWeight int     `json:"effective_weight"`
	Multiplier      float64 `json:"multiplier"`
	SampleCount     int     `json:"sample_count"`
	MinSamples      int     `json:"min_samples"`
	MedianFRTMs     int64   `json:"median_frt_ms"`
	SuccessRate     float64 `json:"success_rate"`
	Rate429         float64 `json:"rate_429"`
	Rate5xx         float64 `json:"rate_5xx"`
	Ready           bool    `json:"ready"`
	Source          string  `json:"source"`
}

type channelRuntimeSampleCacheEntry struct {
	samples   []channelRuntimeSample
	source    string
	expiresAt time.Time
}

func samplesAfterCutoff(samples []channelRuntimeSample, cutoff int64) []channelRuntimeSample {
	filtered := make([]channelRuntimeSample, 0, len(samples))
	for _, sample := range samples {
		if sample.At >= cutoff {
			filtered = append(filtered, sample)
		}
	}
	return filtered
}

var channelRuntimeScores = struct {
	sync.RWMutex
	items map[string][]channelRuntimeSample
}{items: make(map[string][]channelRuntimeSample)}

var channelRuntimeSampleCache = struct {
	sync.RWMutex
	items map[string]channelRuntimeSampleCacheEntry
}{items: make(map[string]channelRuntimeSampleCacheEntry)}

const channelRuntimeSampleCacheTTL = 2 * time.Second
const channelRuntimeRedisReadTimeout = 150 * time.Millisecond
const channelRuntimeRedisWriteTimeout = 150 * time.Millisecond

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
			ctx, cancel := context.WithTimeout(context.Background(), channelRuntimeRedisWriteTimeout)
			defer cancel()
			pipe := common.RDB.TxPipeline()
			pipe.RPush(ctx, redisKey, string(payload))
			pipe.LTrim(ctx, redisKey, -2000, -1)
			pipe.Expire(ctx, redisKey, window+time.Minute)
			if _, err = pipe.Exec(ctx); err != nil {
				common.SysError("dynamic channel weight redis write failed")
			}
			channelRuntimeSampleCache.Lock()
			delete(channelRuntimeSampleCache.items, redisKey)
			channelRuntimeSampleCache.Unlock()
		}
	}
}

func localChannelRuntimeSamples(group, modelName string, channelID int, cutoff int64) []channelRuntimeSample {
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

func loadChannelRuntimeSamples(group, modelName string, channelID int, cutoff int64) ([]channelRuntimeSample, string) {
	if common.RedisEnabled && common.RDB != nil {
		redisKey := channelRuntimeRedisKey(group, modelName, channelID)
		now := time.Now()
		channelRuntimeSampleCache.RLock()
		cached, found := channelRuntimeSampleCache.items[redisKey]
		channelRuntimeSampleCache.RUnlock()
		if found && now.Before(cached.expiresAt) {
			return samplesAfterCutoff(cached.samples, cutoff), cached.source
		}

		ctx, cancel := context.WithTimeout(context.Background(), channelRuntimeRedisReadTimeout)
		values, err := common.RDB.LRange(ctx, redisKey, 0, -1).Result()
		cancel()
		if err == nil {
			samples := make([]channelRuntimeSample, 0, len(values))
			for _, value := range values {
				var sample channelRuntimeSample
				if common.UnmarshalJsonStr(value, &sample) == nil && sample.At >= cutoff {
					samples = append(samples, sample)
				}
			}
			if len(samples) == 0 {
				samples = localChannelRuntimeSamples(group, modelName, channelID, cutoff)
			}
			source := "redis"
			if len(values) == 0 && len(samples) > 0 {
				source = "memory"
			}
			channelRuntimeSampleCache.Lock()
			channelRuntimeSampleCache.items[redisKey] = channelRuntimeSampleCacheEntry{
				samples: samples, source: source, expiresAt: now.Add(channelRuntimeSampleCacheTTL),
			}
			channelRuntimeSampleCache.Unlock()
			return samplesAfterCutoff(samples, cutoff), source
		}
		common.SysError("dynamic channel weight redis read failed: " + err.Error())
		samples := localChannelRuntimeSamples(group, modelName, channelID, cutoff)
		channelRuntimeSampleCache.Lock()
		channelRuntimeSampleCache.items[redisKey] = channelRuntimeSampleCacheEntry{
			samples: samples, source: "memory_fallback", expiresAt: now.Add(channelRuntimeSampleCacheTTL),
		}
		channelRuntimeSampleCache.Unlock()
		return samples, "memory_fallback"
	}
	return localChannelRuntimeSamples(group, modelName, channelID, cutoff), "memory"
}

// EffectiveChannelWeight returns the configured weight adjusted by recent
// runtime health. Priority is deliberately ignored here: callers retain the
// existing priority tiers and only use this value within one tier.
func EffectiveChannelWeight(ch *model.Channel, group, modelName string) int {
	return CalculateChannelRuntimeScore(ch, group, modelName).EffectiveWeight
}

// CalculateChannelRuntimeScore returns both the effective weight and the
// evidence behind it so routing decisions can be audited after the request.
func CalculateChannelRuntimeScore(ch *model.Channel, group, modelName string) ChannelRuntimeScore {
	if ch == nil {
		return ChannelRuntimeScore{}
	}
	base := ch.GetWeight()
	scoreSnapshot := ChannelRuntimeScore{ChannelID: ch.Id, BaseWeight: base, EffectiveWeight: base, Multiplier: 1, Source: "disabled"}
	if !operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled {
		return scoreSnapshot
	}
	setting := operation_setting.GetMonitorSetting()
	window := time.Duration(setting.DynamicChannelWeightWindowMinutes) * time.Minute
	if window <= 0 {
		window = 15 * time.Minute
	}
	cutoff := time.Now().Add(-window)
	samples, source := loadChannelRuntimeSamples(group, modelName, ch.Id, cutoff.UnixMilli())
	scoreSnapshot.Source = source
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
	if minSamples < 1 {
		minSamples = 1
	}
	scoreSnapshot.SampleCount = count
	scoreSnapshot.MinSamples = minSamples
	if count > 0 {
		scoreSnapshot.SuccessRate = float64(successCount) / float64(count)
		scoreSnapshot.Rate429 = float64(throttledCount) / float64(count)
		scoreSnapshot.Rate5xx = float64(serverErrorCount) / float64(count)
	}
	if len(frts) > 0 {
		sort.Slice(frts, func(i, j int) bool { return frts[i] < frts[j] })
		scoreSnapshot.MedianFRTMs = frts[len(frts)/2].Milliseconds()
	}
	if count < minSamples {
		return scoreSnapshot
	}
	scoreSnapshot.Ready = true

	successRate := scoreSnapshot.SuccessRate
	frtScore := 1.0
	if len(frts) > 0 {
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
	scoreSnapshot.EffectiveWeight = weight
	if scoreSnapshot.BaseWeight > 0 {
		scoreSnapshot.Multiplier = float64(weight) / float64(scoreSnapshot.BaseWeight)
	} else {
		scoreSnapshot.Multiplier = score
	}
	return scoreSnapshot
}

// resetChannelRuntimeScores is test-only support kept private to avoid making
// the runtime score store part of the public API.
func resetChannelRuntimeScores() {
	channelRuntimeScores.Lock()
	channelRuntimeScores.items = make(map[string][]channelRuntimeSample)
	channelRuntimeScores.Unlock()
	channelRuntimeSampleCache.Lock()
	channelRuntimeSampleCache.items = make(map[string]channelRuntimeSampleCacheEntry)
	channelRuntimeSampleCache.Unlock()
}
