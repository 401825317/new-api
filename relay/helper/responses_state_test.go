package helper

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
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

	reasoning, compaction, err := StripForeignResponsesState(&request)
	require.NoError(t, err)
	require.Equal(t, 1, reasoning)
	require.Zero(t, compaction)

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
	reasoning, compaction, err := StripForeignResponsesState(&request)
	require.NoError(t, err)
	require.Zero(t, reasoning)
	require.Zero(t, compaction)
	require.Equal(t, original, request.Input)
}
