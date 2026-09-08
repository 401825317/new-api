package service

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

func ResponsesRecoveryEnabled(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Method == http.MethodPost && c.Request.URL.Path == "/v1/responses" && (c.GetBool("responses_recovery_stream_request") || c.GetBool(string(constant.ContextKeyIsStream))) && common.GetEnvOrDefaultBool("RESPONSES_STREAM_RECOVERY_ENABLED", false)
}

var recoveryCooldownOnce sync.Once
var recoveryCooldown *cachex.HybridCache[int]

func responsesCooldownCache() *cachex.HybridCache[int] {
	recoveryCooldownOnce.Do(func() {
		recoveryCooldown = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace("new-api:responses-recovery:v1"),
			Redis:     common.RDB, RedisCodec: cachex.IntCodec{},
			RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, 10000).WithTTL(30 * time.Second).Build()
			},
		})
	})
	return recoveryCooldown
}

func responsesRouteKey(group, modelName string, id int) string {
	return fmt.Sprintf("%x:%d", sha256.Sum256([]byte(group+"\x00"+modelName)), id)
}

func ResponsesRouteCooling(group, modelName string, id int) bool {
	_, found, err := responsesCooldownCache().Get(responsesRouteKey(group, modelName, id))
	if err != nil {
		common.SysError("responses recovery cooldown read failed")
	}
	return found
}

func selectResponsesRecoveryChannel(c *gin.Context, group, modelName string, retry int) (*model.Channel, error) {
	if !ResponsesRecoveryEnabled(c) {
		if operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled {
			return selectDynamicChannel(c, group, modelName, retry)
		}
		return model.GetRandomSatisfiedChannel(group, modelName, retry)
	}
	candidates, err := model.RecoveryCandidates(group, modelName)
	if err != nil {
		return nil, err
	}
	used := make(map[string]bool)
	for _, id := range c.GetStringSlice("use_channel") {
		used[id] = true
	}
	var eligible []*model.Channel
	var priority int64
	for _, ch := range candidates {
		if !ResponsesRecoveryChannelSupported(ch) {
			continue
		}
		if used[strconv.Itoa(ch.Id)] || ResponsesRouteCooling(group, modelName, ch.Id) {
			continue
		}
		if len(eligible) == 0 || ch.GetPriority() > priority {
			eligible = nil
			priority = ch.GetPriority()
		}
		if ch.GetPriority() == priority {
			eligible = append(eligible, ch)
		}
	}
	if len(eligible) == 0 {
		c.Set("responses_recovery_exhausted", true)
		return nil, nil
	}
	c.Set("responses_recovery_exhausted", false)
	if operation_setting.GetMonitorSetting().DynamicChannelWeightEnabled {
		return selectByDynamicWeight(c, group, modelName, retry, priority, "recovery", eligible), nil
	}
	weight := 0
	weightOf := func(ch *model.Channel) int {
		if !common.MemoryCacheEnabled {
			return ch.GetWeight() + 10
		}
		return ch.GetWeight()
	}
	for _, ch := range eligible {
		weight += weightOf(ch)
	}
	if weight == 0 {
		return eligible[common.GetRandomInt(len(eligible))], nil
	}
	n := common.GetRandomInt(weight)
	for _, ch := range eligible {
		n -= weightOf(ch)
		if n < 0 {
			return ch, nil
		}
	}
	return nil, nil
}

// selectDynamicChannel applies runtime health weighting to the ordinary
// selection path as well as Responses recovery. Priority tiers remain
// authoritative; dynamic health only changes probability within the selected
// tier so a degraded channel cannot silently outrank an explicitly preferred
// tier.
func selectDynamicChannel(c *gin.Context, group, modelName string, retry int) (*model.Channel, error) {
	candidates, err := model.RecoveryCandidates(group, modelName)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	priorities := make(map[int64]struct{}, len(candidates))
	for _, ch := range candidates {
		priorities[ch.GetPriority()] = struct{}{}
	}
	sortedPriorities := make([]int64, 0, len(priorities))
	for priority := range priorities {
		sortedPriorities = append(sortedPriorities, priority)
	}
	// sort.Slice is used instead of relying on database/cache ordering.
	sort.Slice(sortedPriorities, func(i, j int) bool { return sortedPriorities[i] > sortedPriorities[j] })
	if retry < 0 {
		retry = 0
	}
	if retry >= len(sortedPriorities) {
		retry = len(sortedPriorities) - 1
	}
	targetPriority := sortedPriorities[retry]
	eligible := make([]*model.Channel, 0, len(candidates))
	for _, ch := range candidates {
		if ch.GetPriority() != targetPriority {
			continue
		}
		eligible = append(eligible, ch)
	}
	if len(eligible) == 0 {
		return nil, nil
	}
	return selectByDynamicWeight(c, group, modelName, retry, targetPriority, "standard", eligible), nil
}

const dynamicChannelWeightAuditContextKey = "dynamic_channel_weight_audit"

// DynamicChannelWeightAudit records the exact candidate weights used by one
// selection. It is attached only to administrator-visible log metadata.
type DynamicChannelWeightAudit struct {
	Group               string                `json:"group"`
	Model               string                `json:"model"`
	Mode                string                `json:"mode"`
	Retry               int                   `json:"retry"`
	Priority            int64                 `json:"priority"`
	SelectedChannelID   int                   `json:"selected_channel_id"`
	SelectedProbability float64               `json:"selected_probability"`
	Candidates          []ChannelRuntimeScore `json:"candidates"`
}

func selectByDynamicWeight(c *gin.Context, group, modelName string, retry int, priority int64, mode string, eligible []*model.Channel) *model.Channel {
	if len(eligible) == 0 {
		return nil
	}
	scores := make([]ChannelRuntimeScore, 0, len(eligible))
	total := 0
	for _, ch := range eligible {
		score := CalculateChannelRuntimeScore(ch, group, modelName)
		if score.EffectiveWeight > 0 {
			total += score.EffectiveWeight
		}
		scores = append(scores, score)
	}
	if total == 0 {
		// Preserve New API's equal-probability behavior when every configured
		// weight is zero, and expose the actual selection weights in the audit.
		for i := range scores {
			scores[i].EffectiveWeight = 100
			scores[i].Multiplier = 1
		}
		total = len(scores) * 100
	}
	n := common.GetRandomInt(total)
	selectedIndex := len(eligible) - 1
	for i := range eligible {
		n -= scores[i].EffectiveWeight
		if n < 0 {
			selectedIndex = i
			break
		}
	}
	selected := eligible[selectedIndex]
	audit := DynamicChannelWeightAudit{
		Group:               group,
		Model:               modelName,
		Mode:                mode,
		Retry:               retry,
		Priority:            priority,
		SelectedChannelID:   selected.Id,
		SelectedProbability: float64(scores[selectedIndex].EffectiveWeight) / float64(total),
		Candidates:          scores,
	}
	if c != nil {
		audits, _ := c.Get(dynamicChannelWeightAuditContextKey)
		selectionAudits, _ := audits.([]DynamicChannelWeightAudit)
		if len(selectionAudits) < 16 {
			selectionAudits = append(selectionAudits, audit)
			c.Set(dynamicChannelWeightAuditContextKey, selectionAudits)
		}
		selectedScore := scores[selectedIndex]
		logger.LogInfo(c, fmt.Sprintf("dynamic channel weight selected: mode=%s group=%s model=%s retry=%d priority=%d channel=%d base=%d effective=%d multiplier=%.4f probability=%.4f samples=%d/%d median_frt_ms=%d success_rate=%.4f rate_429=%.4f rate_5xx=%.4f source=%s",
			mode, group, modelName, retry, priority, selected.Id, selectedScore.BaseWeight, selectedScore.EffectiveWeight,
			selectedScore.Multiplier, audit.SelectedProbability, selectedScore.SampleCount, selectedScore.MinSamples,
			selectedScore.MedianFRTMs, selectedScore.SuccessRate, selectedScore.Rate429, selectedScore.Rate5xx, selectedScore.Source))
	}
	return selected
}

// AppendDynamicChannelWeightAdminInfo adds all decisions made for the current
// request to usage/error logs without exposing them to non-admin clients.
func AppendDynamicChannelWeightAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	if audits, ok := c.Get(dynamicChannelWeightAuditContextKey); ok {
		adminInfo["dynamic_channel_weight"] = audits
	}
}

// ResponsesRecoveryChannelSupported gates recovery to adaptors that can
// consume and emit the OpenAI Responses protocol. Claude/Gemini/Qwen channels
// may support other endpoints, but their Responses converters are not
// complete and must not receive a replayed Responses body.
func ResponsesRecoveryChannelSupported(ch *model.Channel) bool {
	if ch == nil {
		return false
	}
	apiType, ok := common.ChannelType2APIType(ch.Type)
	if !ok {
		return false
	}
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
		constant.APITypeDeepSeek, constant.APITypeXai, constant.APITypeCodex:
		return true
	default:
		return false
	}
}

// Only transient upstream failures penalize the route; invalid inputs and client
// cancellation must not poison channel health. Never globally purge affinity.
func HandleResponsesRecoveryFailure(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	if c == nil || c.Request == nil || c.Request.Context().Err() != nil {
		return
	}
	if info == nil {
		return
	}
	penalize := false
	if info.ResponsesRecovery != nil {
		err = info.ResponsesRecovery.Error
		penalize = info.ResponsesRecovery.Penalize
	} else if err != nil {
		penalize = err.StatusCode == 429 || err.StatusCode >= 500
	}
	if err == nil {
		return
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	if penalize && !info.IsChannelTest {
		ObserveChannelRuntimeResult(group, info.OriginModelName, info.ChannelId, responseFRT(info), err.StatusCode, false)
	}
	if !ResponsesRecoveryEnabled(c) {
		return
	}
	c.Set("responses_recovery_failed", true)
	if !penalize {
		return
	}
	seconds := common.GetEnvOrDefault("RESPONSES_RECOVERY_COOLDOWN_SECONDS", 30)
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 300 {
		seconds = 300
	}
	if e := responsesCooldownCache().SetWithTTL(responsesRouteKey(group, info.OriginModelName, info.ChannelId), 1, time.Duration(seconds)*time.Second); e != nil {
		common.SysError("responses recovery cooldown write failed")
	}
	// The route cooldown is a temporary affinity tombstone. Do not GET/DELETE
	// the binding: that races with another request's successful replacement.
	// Failed requests cannot refresh it; a successful fallback replaces it.
}

func responseFRT(info *relaycommon.RelayInfo) time.Duration {
	if info == nil {
		return 0
	}
	return info.ChannelAttemptFRT()
}
