package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoUsesUpstreamFirstResponseLatency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	start := time.Unix(1700000000, 0)
	upstreamStart := start.Add(10 * time.Second)
	firstResponse := upstreamStart.Add(4 * time.Second)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		FirstResponseTime:        firstResponse,
		ChannelMeta:              &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, float64(4000), other["frt"])
	require.Equal(t, float64(14000), other["end_to_end_frt"])
	require.Equal(t, float64(10000), other["pre_upstream_ms"])
}

func TestGenerateTextOtherInfoFirstResponseLatencyFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	start := time.Unix(1700000000, 0)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         start,
		FirstResponseTime: start.Add(3 * time.Second),
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, float64(3000), other["frt"])
	require.Equal(t, float64(3000), other["end_to_end_frt"])
	_, ok := other["pre_upstream_ms"]
	require.False(t, ok)
}
