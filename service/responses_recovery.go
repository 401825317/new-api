package service

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
		return nil, nil
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

// Only transient upstream failures penalize the route; invalid inputs and client
// cancellation must not poison channel health. Never globally purge affinity.
func HandleResponsesRecoveryFailure(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	if !ResponsesRecoveryEnabled(c) || c.Request.Context().Err() != nil {
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
	c.Set("responses_recovery_failed", true)
	if !penalize {
		return
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
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
