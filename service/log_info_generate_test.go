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

func TestGenerateTextOtherInfoStreamUsesUpstreamResponseLatencyForFRT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	start := time.Unix(1700000000, 0)
	upstreamStart := start.Add(10 * time.Second)
	upstreamResponse := upstreamStart.Add(2 * time.Second)
	firstResponse := upstreamStart.Add(4 * time.Second)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		UpstreamResponseTime:     upstreamResponse,
		FirstResponseTime:        firstResponse,
		FirstStreamDataTime:      firstResponse,
		FirstStreamContentTime:   upstreamStart.Add(6 * time.Second),
		IsStream:                 true,
		ChannelMeta:              &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, float64(2000), other["frt"])
	require.Equal(t, "upstream_response", other["frt_source"])
	require.Equal(t, float64(2000), other["upstream_response_ms"])
	require.Equal(t, float64(12000), other["end_to_end_upstream_response_ms"])
	require.Equal(t, float64(4000), other["stream_first_data_ms"])
	require.Equal(t, float64(6000), other["stream_first_content_ms"])
	require.Equal(t, float64(14000), other["end_to_end_frt"])
	require.Equal(t, float64(10000), other["pre_upstream_ms"])
}

func TestGenerateTextOtherInfoNonStreamKeepsFirstDataFRT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	start := time.Unix(1700000000, 0)
	upstreamStart := start.Add(time.Second)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		UpstreamResponseTime:     upstreamStart.Add(500 * time.Millisecond),
		FirstResponseTime:        start.Add(3 * time.Second),
		ChannelMeta:              &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, float64(2000), other["frt"])
	require.Equal(t, "upstream_first_data", other["frt_source"])
	require.Equal(t, float64(3000), other["end_to_end_frt"])
	require.Equal(t, float64(1000), other["pre_upstream_ms"])
	require.NotContains(t, other, "upstream_response_ms")
	require.NotContains(t, other, "stream_first_data_ms")
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
	require.Equal(t, "upstream_first_data", other["frt_source"])
	require.Equal(t, float64(3000), other["end_to_end_frt"])
	require.NotContains(t, other, "pre_upstream_ms")
}
