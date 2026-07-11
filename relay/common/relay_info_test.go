package common

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqUnmarshalInputReferenceString(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, json.Unmarshal([]byte(`{
  "prompt": "make a video",
  "model": "grok-image-video",
  "input_reference": "data:image/png;base64,AAAA"
}`), &req))

	require.Equal(t, "data:image/png;base64,AAAA", req.InputReference)
}

func TestTaskSubmitReqUnmarshalOpenClawInputReferenceObject(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, json.Unmarshal([]byte(`{
  "prompt": "make a video",
  "model": "grok-image-video",
  "input_reference": {"image_url":"data:image/png;base64,AAAA"}
}`), &req))

	require.Equal(t, "data:image/png;base64,AAAA", req.InputReference)
}

func TestTaskSubmitReqRejectsInvalidInputReferenceObject(t *testing.T) {
	var req TaskSubmitReq
	err := json.Unmarshal([]byte(`{
  "prompt": "make a video",
  "model": "grok-image-video",
  "input_reference": {"url":"https://example.com/reference.png"}
}`), &req)

	require.EqualError(t, err, "input_reference object requires a non-empty image_url")
}

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
	upstreamResponse := upstreamStart.Add(2 * time.Second)
	firstResponse := upstreamStart.Add(4 * time.Second)
	info := &RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: upstreamStart,
		UpstreamResponseTime:     upstreamResponse,
		FirstResponseTime:        firstResponse,
		FirstStreamDataTime:      firstResponse,
		FirstStreamContentTime:   upstreamStart.Add(6 * time.Second),
	}

	require.Equal(t, int64(14000), info.EndToEndFirstResponseLatencyMs())
	require.Equal(t, int64(4000), info.UpstreamFirstResponseLatencyMs())
	require.Equal(t, int64(2000), info.UpstreamResponseLatencyMs())
	require.Equal(t, int64(12000), info.EndToEndUpstreamResponseLatencyMs())
	require.Equal(t, int64(4000), info.UpstreamFirstStreamDataLatencyMs())
	require.Equal(t, int64(6000), info.UpstreamFirstStreamContentLatencyMs())
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

func TestRelayInfoSetUpstreamResponseTimeRecordsOnce(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	info := &RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: start.Add(time.Second),
	}

	info.SetUpstreamResponseTime()
	first := info.UpstreamResponseTime
	time.Sleep(time.Millisecond)
	info.SetUpstreamResponseTime()

	require.False(t, first.IsZero())
	require.Equal(t, first, info.UpstreamResponseTime)
	require.GreaterOrEqual(t, info.UpstreamResponseLatencyMs(), int64(0))
}

func TestRelayInfoSetFirstStreamDataTimeTracksContentSeparately(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	info := &RelayInfo{
		StartTime:                start,
		UpstreamRequestStartTime: start.Add(time.Second),
	}

	info.SetFirstStreamDataTime(false)
	firstData := info.FirstStreamDataTime
	require.False(t, firstData.IsZero())
	require.True(t, info.FirstStreamContentTime.IsZero())

	time.Sleep(time.Millisecond)
	info.SetFirstStreamDataTime(true)

	require.Equal(t, firstData, info.FirstStreamDataTime)
	require.False(t, info.FirstStreamContentTime.IsZero())
	require.GreaterOrEqual(t, info.UpstreamFirstStreamContentLatencyMs(), info.UpstreamFirstStreamDataLatencyMs())
}
