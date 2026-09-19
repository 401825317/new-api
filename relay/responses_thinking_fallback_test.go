package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const thinkingRejection = `{"error":{"type":"invalid_request_error","message":"The reasoning_text in the thinking mode must be passed back to the API"}}`
const thinkingPayload = `{"model":"deepseek-v4-pro","reasoning":{"effort":"high"},"previous_response_id":"resp_previous","input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`

func TestResponsesThinkingFallbackMissingStateVariants(t *testing.T) {
	for _, field := range []string{"reasoning_text", "reasoning_content"} {
		for _, quoted := range []bool{false, true} {
			fieldName := field
			if quoted {
				fieldName = "`" + field + "`"
			}
			t.Run(fieldName, func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeDeepSeek, ChannelType: constant.ChannelTypeDeepSeek, ChannelId: 27,
				}}
				payload := []byte(strings.ReplaceAll(thinkingPayload, "deepseek-v4-pro", "deepseek-v4.1-flash"))
				errorBody := strings.Replace(thinkingRejection, "The reasoning_text", "Upstream request failed: [invalid_request_error] The "+fieldName, 1)
				calls := 0
				result, err := responsesRequestWithThinkingFallback(ctx, info, bytes.NewReader(payload), payload, func(body io.Reader) (any, error) {
					calls++
					data, readErr := io.ReadAll(body)
					require.NoError(t, readErr)
					require.Equal(t, 27, info.ChannelId)
					require.Equal(t, "resp_previous", gjson.GetBytes(data, "previous_response_id").String())
					require.JSONEq(t, gjson.GetBytes(payload, "input").Raw, gjson.GetBytes(data, "input").Raw)
					if calls == 1 {
						return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(errorBody))}, nil
					}
					require.Equal(t, "none", gjson.GetBytes(data, "reasoning.effort").String())
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
				})
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.Equal(t, 200, result.(*http.Response).StatusCode)
			})
		}
	}
}

// TestResponsesThinkingFallback exercises the actual send boundary: no channel
// selection occurs, only the final payload's effort changes, and a second
// rejection cannot recursively replay tool/continuation history.
func TestResponsesThinkingFallback(t *testing.T) {
	for _, modelName := range []string{"deepseek-v4-pro", "deepseek-v4.1-flash"} {
		for _, secondStatus := range []int{200, 400} {
			t.Run(modelName+"/"+http.StatusText(secondStatus), func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeDeepSeek, ChannelType: constant.ChannelTypeDeepSeek, ChannelId: 42,
				}}
				originalPayload := strings.ReplaceAll(thinkingPayload, "deepseek-v4-pro", modelName)
				payload := []byte(originalPayload)
				calls := 0
				firstBody := &thinkingTrackedBody{Reader: bytes.NewReader([]byte(thinkingRejection))}
				result, err := responsesRequestWithThinkingFallback(ctx, info, bytes.NewReader(payload), payload, func(body io.Reader) (any, error) {
					calls++
					data, readErr := io.ReadAll(body)
					require.NoError(t, readErr)
					require.Equal(t, 42, info.ChannelId)
					if calls == 1 {
						require.JSONEq(t, originalPayload, string(data))
						return &http.Response{StatusCode: 400, Body: firstBody}, nil
					}
					require.True(t, firstBody.closed)
					require.Equal(t, "none", gjson.GetBytes(data, "reasoning.effort").String())
					require.Equal(t, modelName, gjson.GetBytes(data, "model").String())
					require.Equal(t, "resp_previous", gjson.GetBytes(data, "previous_response_id").String())
					require.JSONEq(t, gjson.Get(thinkingPayload, "input").Raw, gjson.GetBytes(data, "input").Raw)
					return &http.Response{StatusCode: secondStatus, Body: io.NopCloser(bytes.NewReader([]byte(thinkingRejection)))}, nil
				})
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.Equal(t, secondStatus, result.(*http.Response).StatusCode)
				require.Equal(t, originalPayload, string(payload))
				require.True(t, ctx.GetBool("responses_thinking_fallback_used"))
			})
		}
	}
}

// TestResponsesThinkingFallbackGuards verifies fail-closed evidence and checks
// that inspected error bodies remain readable by the ordinary error handler.
func TestResponsesThinkingFallbackGuards(t *testing.T) {
	for _, scenario := range []string{"ordinary400", "contextLimit", "contentUsage", "contentOutput", "usage", "output", "malformed", "oversized", "sse", "500", "compatible", "unknownModel", "disabled", "written", "committed", "received", "cancelled", "used", "compact"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{
				ApiType: constant.APITypeDeepSeek, ChannelType: constant.ChannelTypeDeepSeek,
			}}
			payload := []byte(thinkingPayload)
			errorBody := thinkingRejection
			status := 400
			switch scenario {
			case "ordinary400":
				errorBody = `{"error":{"message":"invalid reasoning_text"}}`
			case "contextLimit":
				errorBody = `{"error":{"message":"Input exceeds the context limit (1048566 tokens). Please shorten the input."}}`
			case "contentUsage":
				errorBody = strings.ReplaceAll(thinkingRejection[:len(thinkingRejection)-1], "reasoning_text", "`reasoning_content`") + `,"usage":{"total_tokens":0}}`
			case "contentOutput":
				errorBody = strings.ReplaceAll(thinkingRejection[:len(thinkingRejection)-1], "reasoning_text", "`reasoning_content`") + `,"output":[{"text":"done"}]}`
			case "usage":
				errorBody = thinkingRejection[:len(thinkingRejection)-1] + `,"usage":{"total_tokens":0}}`
			case "output":
				errorBody = thinkingRejection[:len(thinkingRejection)-1] + `,"output":[{"text":"done"}]}`
			case "malformed":
				errorBody = thinkingRejection + "invalid"
			case "oversized":
				errorBody += string(bytes.Repeat([]byte(" "), 64<<10))
			case "sse":
				status = 200
				errorBody = "event: response.failed\ndata: {\"type\":\"response.failed\",\"status_code\":400,\"response\":" + thinkingRejection + "}\n\n"
			case "500":
				status = 500
			case "compatible":
				info.ApiType = constant.APITypeOpenAI
				info.ChannelType = constant.ChannelTypeOpenAI
			case "unknownModel":
				payload = []byte(`{"model":"deepseek-custom"}`)
			case "disabled":
				payload = []byte(`{"model":"deepseek-v4-pro","reasoning":{"effort":"none"}}`)
			case "written":
				ctx.Writer.WriteHeaderNow()
			case "committed":
				info.ResponsesRecovery = &relaycommon.ResponsesRecoveryOutcome{Committed: true}
			case "received":
				info.ReceivedResponseCount = 1
			case "cancelled":
				cancelCtx, cancel := context.WithCancel(ctx.Request.Context())
				cancel()
				ctx.Request = ctx.Request.WithContext(cancelCtx)
			case "used":
				ctx.Set("responses_thinking_fallback_used", true)
			case "compact":
				info.RelayMode = relayconstant.RelayModeResponsesCompact
			}
			calls := 0
			result, err := responsesRequestWithThinkingFallback(ctx, info, bytes.NewReader(payload), payload, func(io.Reader) (any, error) {
				calls++
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader([]byte(errorBody)))}, nil
			})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			remaining, err := io.ReadAll(result.(*http.Response).Body)
			require.NoError(t, err)
			require.Equal(t, errorBody, string(remaining))
		})
	}
}

// thinkingTrackedBody checks that rejected connections are closed before reuse
// of the selected upstream route.
type thinkingTrackedBody struct {
	io.Reader
	closed bool
}

func (body *thinkingTrackedBody) Close() error {
	body.closed = true
	return nil
}
