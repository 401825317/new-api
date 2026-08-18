package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestResponsesRequestCompatibilityNormalization protects strict Responses
// upstreams from replay metadata while preserving tool-result business payloads.
func TestResponsesRequestCompatibilityNormalization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"system","status":"completed","content":[{"type":"input_text","text":"instructions"}]},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"prior answer"}]},
			{"type":"function_call_output","call_id":"call_1","output":{"status":"done","value":"preserve me"}}
		]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesRequest(c)
	require.NoError(t, err)

	var input []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &input))
	require.Equal(t, "developer", input[0]["role"])
	_, hasFirstStatus := input[0]["status"]
	_, hasSecondStatus := input[1]["status"]
	require.False(t, hasFirstStatus)
	require.False(t, hasSecondStatus)

	toolOutput, ok := input[2]["output"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "done", toolOutput["status"])
}

func TestNormalizeResponsesRequestBodyPreservesPassThroughFields(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"custom_extension":{"status":"keep"},
		"input":[
			{"type":"message","role":"system","status":"completed","content":"instructions"},
			{"type":"function_call_output","output":{"status":"done"}}
		]
	}`)

	normalizedBody, changed, err := dto.NormalizeResponsesRequestBody(rawBody)
	require.NoError(t, err)
	require.True(t, changed)

	var body map[string]any
	require.NoError(t, common.Unmarshal(normalizedBody, &body))
	require.Equal(t, "gpt-5.6-sol", body["model"])
	customExtension, ok := body["custom_extension"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "keep", customExtension["status"])

	input, ok := body["input"].([]any)
	require.True(t, ok)
	systemMessage, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "developer", systemMessage["role"])
	_, hasStatus := systemMessage["status"]
	require.False(t, hasStatus)

	toolOutputItem, ok := input[1].(map[string]any)
	require.True(t, ok)
	toolOutput, ok := toolOutputItem["output"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "done", toolOutput["status"])
}
