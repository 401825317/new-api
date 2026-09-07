package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestStripsProviderBoundState(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
		{"type":"reasoning","id":"rs_openai","status":"completed","summary":[],"encrypted_content":"opaque-openai"},
		{"type":"compaction","id":"cmp_openai","encrypted_content":"opaque-compaction"},
		{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}
	]`)
	request := dto.OpenAIResponsesRequest{
		Model:     "deepseek-v4-pro",
		Input:     input,
		Reasoning: &dto.Reasoning{Effort: "medium"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "deepseek-v4-pro"}}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, got.Reasoning)
	require.Equal(t, "none", got.Reasoning.Effort)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(got.Input, &items))
	require.Len(t, items, 3)
	require.Equal(t, "message", items[0]["type"])
	require.Equal(t, "function_call", items[1]["type"])
	require.Equal(t, "function_call_output", items[2]["type"])
}

func TestConvertOpenAIResponsesRequestPreservesDeepSeekReasoningText(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","id":"rs_deepseek","content":[{"type":"reasoning_text","text":"visible DeepSeek state"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}
	]`)
	request := dto.OpenAIResponsesRequest{
		Model:     "deepseek-v4-pro",
		Input:     input,
		Reasoning: &dto.Reasoning{Effort: "medium"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "deepseek-v4-pro"}}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.JSONEq(t, string(input), string(got.Input))
	require.Equal(t, "medium", got.Reasoning.Effort)
}

func TestStripForeignResponsesStateLeavesStringInputUntouched(t *testing.T) {
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}
	original := append(json.RawMessage(nil), request.Input...)

	reasoning, compaction, err := stripForeignResponsesState(&request)
	require.NoError(t, err)
	require.Zero(t, reasoning)
	require.Zero(t, compaction)
	require.Equal(t, original, request.Input)
}
