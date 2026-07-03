package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetry_SkipRuleAfterStreamStarted(t *testing.T) {
	orig := operation_setting.AutomaticSkipRetryRules
	t.Cleanup(func() { operation_setting.AutomaticSkipRetryRules = orig })
	require.NoError(t, operation_setting.AutomaticSkipRetryRulesFromString(`[{"name":"client-gone","after_stream_started":true,"stream_end_reasons":["client_gone"],"message_contains":["context canceled"]}]`))

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ReceivedResponseCount: 1,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
	err := types.NewOpenAIError(
		fmt.Errorf("request context done: %w", context.Canceled),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	require.False(t, shouldRetry(c, info, err, 1))
}

func TestShouldRetry_RetriesServerErrorBeforeStreamStarted(t *testing.T) {
	orig := operation_setting.AutomaticSkipRetryRules
	t.Cleanup(func() { operation_setting.AutomaticSkipRetryRules = orig })
	require.NoError(t, operation_setting.AutomaticSkipRetryRulesFromString(`[{"name":"client-gone","after_stream_started":true,"stream_end_reasons":["client_gone"],"message_contains":["context canceled"]}]`))

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewOpenAIError(
		fmt.Errorf("temporary upstream failure"),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	require.True(t, shouldRetry(c, &relaycommon.RelayInfo{}, err, 1))
}
