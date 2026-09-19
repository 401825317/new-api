package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/stretchr/testify/require"
)

// DeepSeek 同族回放：推理状态原样送回上游，compaction 仍然丢弃，thinking 保持开启。
// Codex 回放历史时 reasoning item 只有 encrypted_content，这段 blob 只能由 DeepSeek 解密，
// 因此不能按“看起来不透明就丢掉”的通用规则处理。
func TestConvertOpenAIResponsesRequestPassesDeepSeekReasoningThrough(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
		{"type":"reasoning","id":"rs_deepseek","status":"completed","summary":[],"encrypted_content":"dd4e0a2f-0"},
		{"type":"compaction","id":"cmp_openai","encrypted_content":"opaque-compaction"},
		{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}
	]`)
	request := dto.OpenAIResponsesRequest{
		Model:     "deepseek-v4.1-flash",
		Input:     input,
		Reasoning: &dto.Reasoning{Effort: "high"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		UpstreamModelName: "deepseek-v4.1-flash",
	}}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, got.Reasoning)
	require.Equal(t, "high", got.Reasoning.Effort, "passthrough replay must keep thinking enabled")

	var items []map[string]any
	require.NoError(t, json.Unmarshal(got.Input, &items))
	require.Len(t, items, 4)
	require.Equal(t, "reasoning", items[1]["type"])
	require.Equal(t, "rs_deepseek", items[1]["id"])
	require.Equal(t, "dd4e0a2f-0", items[1]["encrypted_content"],
		"DeepSeek-issued encrypted state must reach the upstream unchanged")
	require.Equal(t, "function_call", items[2]["type"])
	require.Equal(t, "function_call_output", items[3]["type"])
}

// 上游家族无法判定为 DeepSeek 系时仍走保守清洗：推理状态整条丢弃，并同步关闭 thinking，
// 避免上游以“reasoning_text 必须回传”拒绝一个无法校验回放的请求。
func TestConvertOpenAIResponsesRequestDisablesThinkingWhenReasoningRemoved(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","id":"rs_other","status":"completed","summary":[],"encrypted_content":"opaque-other"},
		{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"}
	]`)
	request := dto.OpenAIResponsesRequest{
		Model:     "kimi-k2",
		Input:     input,
		Reasoning: &dto.Reasoning{Effort: "medium"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		UpstreamModelName: "kimi-k2",
	}}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, got.Reasoning)
	require.Equal(t, "none", got.Reasoning.Effort)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(got.Input, &items))
	require.Len(t, items, 1)
	require.Equal(t, "function_call", items[0]["type"])
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

func TestConvertResponsesV41ThinkingSuffix(t *testing.T) {
	for _, suffix := range []string{"none", "max"} {
		t.Run(suffix, func(t *testing.T) {
			modelName := "deepseek-v4.1-flash-" + suffix
			request := dto.OpenAIResponsesRequest{Model: modelName, PreviousResponseID: "resp_previous", Input: json.RawMessage(`"continue"`)}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDeepSeek, UpstreamModelName: modelName}}
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
			require.NoError(t, err)
			got := converted.(dto.OpenAIResponsesRequest)
			require.Equal(t, "deepseek-v4.1-flash", got.Model)
			require.Equal(t, suffix, got.Reasoning.Effort)
			require.Equal(t, "resp_previous", got.PreviousResponseID)
			require.Equal(t, request.Input, got.Input)
		})
	}
}

func TestStripForeignResponsesStateLeavesStringInputUntouched(t *testing.T) {
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}
	original := append(json.RawMessage(nil), request.Input...)

	result, err := stripForeignResponsesState(&request, helper.ResponsesStatePolicy{PassThroughReasoning: true})
	require.NoError(t, err)
	require.Zero(t, result.RemovedReasoning)
	require.Zero(t, result.RemovedCompaction)
	require.Equal(t, original, request.Input)
}
