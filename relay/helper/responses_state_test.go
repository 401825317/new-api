package helper

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStripForeignResponsesStatePreservesVisibleContentAndTools(t *testing.T) {
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"message","role":"assistant","content":[
			{"type":"output_text","text":"visible"},
			{"type":"reasoning","encrypted_content":"opaque"}
		]},
		{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}
	]`)}

	result, err := StripForeignResponsesState(&request, ResponsesStatePolicy{})
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedReasoning)
	require.Zero(t, result.RemovedCompaction)
	require.Zero(t, result.PassedThroughReasoning)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(request.Input, &items))
	require.Len(t, items, 3)
	content := items[0]["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "output_text", content[0].(map[string]any)["type"])
	require.Equal(t, "function_call", items[1]["type"])
	require.Equal(t, "function_call_output", items[2]["type"])
}

func TestStripForeignResponsesStateLeavesStringInputUntouched(t *testing.T) {
	original := json.RawMessage(`"hello"`)
	request := dto.OpenAIResponsesRequest{Input: original}
	result, err := StripForeignResponsesState(&request, ResponsesStatePolicy{PassThroughReasoning: true})
	require.NoError(t, err)
	require.Zero(t, result.Removed())
	require.Zero(t, result.PassedThroughReasoning)
	require.Equal(t, original, request.Input)
}

// DeepSeek 系上游在 thinking 模式下要求客户端把上一轮推理状态回传，而 Codex 回放历史时
// 只带 encrypted_content（rollout 记录里 reasoning item 恒为 content 空、encrypted_content
// 非空），这段 blob 又只有该上游自己能解密。因此同族回放必须原样送回，连字节都不能改写。
func TestStripForeignResponsesStatePassesReasoningThroughForDeepSeek(t *testing.T) {
	original := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
		{"type":"reasoning","id":"rs_1","status":"completed","summary":[],"encrypted_content":"dd4e0a2f-0"},
		{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}
	]`)
	request := dto.OpenAIResponsesRequest{Input: original}

	result, err := StripForeignResponsesState(&request, ResponsesStatePolicy{PassThroughReasoning: true})
	require.NoError(t, err)
	require.Zero(t, result.RemovedReasoning)
	require.Equal(t, 1, result.PassedThroughReasoning)
	require.True(t, result.Changed())
	require.Equal(t, string(original), string(request.Input),
		"passthrough must not reserialize the replay payload")
}

// compaction 属于纯 provider 侧状态，没有可读等价物，同族回放也要丢弃；但推理状态不受影响。
func TestStripForeignResponsesStateDropsCompactionForDeepSeek(t *testing.T) {
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"reasoning","id":"rs_deepseek","encrypted_content":"dd4e0a2f-0"},
		{"type":"compaction_summary","encrypted_content":"opaque-compaction"}
	]`)}

	result, err := StripForeignResponsesState(&request, ResponsesStatePolicy{PassThroughReasoning: true})
	require.NoError(t, err)
	require.Zero(t, result.RemovedReasoning)
	require.Equal(t, 1, result.RemovedCompaction)
	require.Equal(t, 1, result.PassedThroughReasoning)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(request.Input, &items))
	require.Len(t, items, 1)
	require.Equal(t, "reasoning", items[0]["type"])
	require.Equal(t, "dd4e0a2f-0", items[0]["encrypted_content"])
}

func TestResponsesStatePolicyForUpstream(t *testing.T) {
	cases := []struct {
		name string
		info *relaycommon.RelayInfo
		want bool
	}{
		{name: "nil info stays conservative", info: nil, want: false},
		{
			name: "native deepseek channel",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDeepSeek},
			},
			want: true,
		},
		{
			name: "openai-compatible channel mapped to deepseek model",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					UpstreamModelName: "deepseek-v4.1-flash",
				},
			},
			want: true,
		},
		{
			name: "openai channel with origin deepseek model",
			info: &relaycommon.RelayInfo{
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
				OriginModelName: "DeepSeek-V4",
			},
			want: true,
		},
		{
			name: "unrelated upstream keeps conservative behaviour",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					UpstreamModelName: "gpt-5.5",
				},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResponsesStatePolicyForUpstream(tc.info).PassThroughReasoning)
		})
	}
}
