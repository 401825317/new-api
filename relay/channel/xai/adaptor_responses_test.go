package xai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestStripsForeignState(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "grok-4-1-fast-reasoning",
		Input: json.RawMessage(`[
			{"type":"message","role":"assistant","content":[
				{"type":"output_text","text":"visible"},
				{"type":"reasoning","encrypted_content":"opaque-openai"}
			]},
			{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call_1"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: request.Model}}, request)
	require.NoError(t, err)

	got := converted.(dto.OpenAIResponsesRequest)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(got.Input, &items))
	require.Len(t, items, 3)
	require.Equal(t, "function_call", items[1]["type"])
	content := items[0]["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "visible", content[0].(map[string]any)["text"])
}
