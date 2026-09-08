package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesRecoveryCooldownIsolation(t *testing.T) {
	t.Setenv("RESPONSES_STREAM_RECOVERY_ENABLED", "true")
	t.Setenv("RESPONSES_RECOVERY_COOLDOWN_SECONDS", "1")
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	require.False(t, ResponsesRecoveryEnabled(c), "non-stream request must stay official")
	c.Set("responses_recovery_stream_request", true)
	require.True(t, ResponsesRecoveryEnabled(c))
	group := fmt.Sprint("cooldown-", time.Now().UnixNano())
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
	info := &relaycommon.RelayInfo{OriginModelName: "model-a", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 71}}
	err := types.NewOpenAIError(fmt.Errorf("overloaded"), types.ErrorCodeBadResponse, 503)
	HandleResponsesRecoveryFailure(c, info, err)
	require.True(t, ResponsesRouteCooling(group, "model-a", 71))
	require.False(t, ResponsesRouteCooling(group, "model-a", 72))
	require.False(t, ResponsesRouteCooling(group, "model-b", 71))
	require.False(t, ResponsesRouteCooling(group+"other", "model-a", 71))
	info.ChannelId = 72
	info.ResponsesRecovery = &relaycommon.ResponsesRecoveryOutcome{Error: err, Penalize: false}
	HandleResponsesRecoveryFailure(c, info, err)
	require.False(t, ResponsesRouteCooling(group, "model-a", 72))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	info.ResponsesRecovery = nil
	HandleResponsesRecoveryFailure(c, info, err)
	require.False(t, ResponsesRouteCooling(group, "model-a", 72))
	require.Eventually(t, func() bool { return !ResponsesRouteCooling(group, "model-a", 71) }, 3*time.Second, 20*time.Millisecond)
}

func TestResponsesRecoveryChannelSupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  int
		want bool
	}{
		{name: "openai", typ: constant.ChannelTypeOpenAI, want: true},
		{name: "deepseek", typ: constant.ChannelTypeDeepSeek, want: true},
		{name: "grok", typ: constant.ChannelTypeXai, want: true},
		{name: "codex", typ: constant.ChannelTypeCodex, want: true},
		{name: "claude", typ: constant.ChannelTypeAnthropic, want: false},
		{name: "gemini", typ: constant.ChannelTypeGemini, want: false},
		{name: "qwen", typ: constant.ChannelTypeAli, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &model.Channel{Type: tc.typ}
			require.Equal(t, tc.want, ResponsesRecoveryChannelSupported(channel))
		})
	}
}

func TestNonTransientFailureDoesNotPoisonDynamicWeight(t *testing.T) {
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
	setting.DynamicChannelWeightMinSamples = 1

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "g")
	info := &relaycommon.RelayInfo{OriginModelName: "m", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 73}}
	badRequest := types.NewOpenAIError(fmt.Errorf("bad request"), types.ErrorCodeBadResponse, 400)
	HandleResponsesRecoveryFailure(c, info, badRequest)

	weight := uint(100)
	score := CalculateChannelRuntimeScore(&model.Channel{Id: 73, Weight: &weight}, "g", "m")
	require.Zero(t, score.SampleCount)
	require.False(t, score.Ready)

	serverError := types.NewOpenAIError(fmt.Errorf("overloaded"), types.ErrorCodeBadResponse, 503)
	HandleResponsesRecoveryFailure(c, info, serverError)
	score = CalculateChannelRuntimeScore(&model.Channel{Id: 73, Weight: &weight}, "g", "m")
	require.Equal(t, 1, score.SampleCount)
	require.True(t, score.Ready)
	require.Equal(t, 1.0, score.Rate5xx)
}

func TestDynamicSelectionStoresExactAudit(t *testing.T) {
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
	setting.DynamicChannelWeightMinSamples = 10

	one := uint(100)
	two := uint(50)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	selected := selectByDynamicWeight(c, "g", "m", 0, 1, "standard", []*model.Channel{
		{Id: 81, Weight: &one},
		{Id: 82, Weight: &two},
	})
	require.NotNil(t, selected)
	value, found := c.Get(dynamicChannelWeightAuditContextKey)
	require.True(t, found)
	audits, ok := value.([]DynamicChannelWeightAudit)
	require.True(t, ok)
	require.Len(t, audits, 1)
	require.Equal(t, selected.Id, audits[0].SelectedChannelID)
	require.Len(t, audits[0].Candidates, 2)
	require.InDelta(t, float64(selected.GetWeight())/150.0, audits[0].SelectedProbability, 0.0001)
}
