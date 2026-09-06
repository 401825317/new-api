package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
