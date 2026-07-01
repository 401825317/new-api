package common

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoFirstResponseLatencyUsesUpstreamStart(t *testing.T) {
	start := time.Unix(1700000000, 0)
	upstreamStart := start.Add(10 * time.Second)
	firstResponse := upstreamStart.Add(4 * time.Second)
	info := &RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		FirstResponseTime:        firstResponse,
	}

	require.Equal(t, int64(14000), info.EndToEndFirstResponseLatencyMs())
	require.Equal(t, int64(4000), info.UpstreamFirstResponseLatencyMs())
	require.Equal(t, int64(10000), info.PreUpstreamLatencyMs())
}

func TestRelayInfoFirstResponseLatencyFallsBackToEndToEnd(t *testing.T) {
	start := time.Unix(1700000000, 0)
	firstResponse := start.Add(3 * time.Second)
	info := &RelayInfo{
		StartTime:         start,
		FirstResponseTime: firstResponse,
	}

	require.Equal(t, int64(3000), info.UpstreamFirstResponseLatencyMs())
	require.Equal(t, int64(0), info.PreUpstreamLatencyMs())
}

func TestRelayInfoSetUpstreamRequestStartTimeKeepsCompletedAttempt(t *testing.T) {
	start := time.Unix(1700000000, 0)
	upstreamStart := start.Add(2 * time.Second)
	info := &RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		FirstResponseTime:        upstreamStart.Add(time.Second),
	}

	info.SetUpstreamRequestStartTime()

	require.Equal(t, upstreamStart, info.UpstreamRequestStartTime)
}

func TestRelayInfoSetUpstreamRequestStartTimeRefreshesBeforeResponse(t *testing.T) {
	info := &RelayInfo{
		StartTime:                time.Now().Add(-time.Second),
		UpstreamRequestStartTime: time.Unix(1, 0),
	}

	info.SetUpstreamRequestStartTime()

	require.True(t, info.UpstreamRequestStartTime.After(time.Unix(1, 0)))
}
