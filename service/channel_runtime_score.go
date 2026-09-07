package service

import (
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// channelRuntimeSample is intentionally small: the scorer only needs a recent
// FRT and outcome, not request content or any credential-related data.
type channelRuntimeSample struct {
	at         time.Time
	frt        time.Duration
	success    bool
	statusCode int
}

var channelRuntimeScores = struct {
	sync.RWMutex
	items map[string][]channelRuntimeSample
}{items: make(map[string][]channelRuntimeSample)}

func channelRuntimeKey(group, modelName string, channelID int) string {
	return group + "\x00" + modelName + "\x00" + strconv.Itoa(channelID)
}

// ObserveChannelRuntimeResult records one completed or failed upstream attempt.
// It is process-local by design; the feature is disabled by default and can be
// enabled after validating the score behavior on the active instance set.
func ObserveChannelRuntimeResult(group, modelName string, channelID int, frt time.Duration, statusCode int, success bool) {
	if !operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled || channelID <= 0 {
		return
	}
	now := time.Now()
	key := channelRuntimeKey(group, modelName, channelID)
	channelRuntimeScores.Lock()
	defer channelRuntimeScores.Unlock()
	window := time.Duration(operation_setting.GetMonitorSetting().DynamicChannelWeightWindowMinutes) * time.Minute
	if window <= 0 {
		window = 15 * time.Minute
	}
	samples := append(channelRuntimeScores.items[key], channelRuntimeSample{at: now, frt: frt, success: success, statusCode: statusCode})
	cutoff := now.Add(-window)
	first := 0
	for first < len(samples) && samples[first].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		samples = samples[first:]
	}
	if len(samples) > 2000 {
		samples = samples[len(samples)-2000:]
	}
	channelRuntimeScores.items[key] = samples
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
	key := channelRuntimeKey(group, modelName, ch.Id)
	channelRuntimeScores.RLock()
	samples := channelRuntimeScores.items[key]
	var count, successCount, throttledCount, serverErrorCount int
	frts := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample.at.Before(cutoff) {
			continue
		}
		count++
		if sample.success {
			successCount++
		}
		if sample.frt > 0 {
			frts = append(frts, sample.frt)
		}
		if sample.statusCode == 429 {
			throttledCount++
		}
		if sample.statusCode >= 500 {
			serverErrorCount++
		}
	}
	channelRuntimeScores.RUnlock()
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
