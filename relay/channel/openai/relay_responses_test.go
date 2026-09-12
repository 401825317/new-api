package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func newResponsesStreamTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-flash",
		},
	}
	return c, recorder, resp, info
}

func responsesSSE(events ...string) string {
	return "data: " + strings.Join(events, "\ndata: ") + "\ndata: [DONE]\n"
}

func TestOaiResponsesStreamHandler_RequiresCompletedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.output_text.delta","delta":"partial output"}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.Error(), "before response.completed")
}

func TestOaiResponsesStreamHandler_RejectsFailedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.output_text.delta","delta":"partial output"}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","message":"upstream 502","code":"server_error"}}}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, "upstream 502", err.ToOpenAIError().Message)
}

func TestOaiResponsesStreamHandler_RejectsCompletedWithNonCompletedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.completed","response":{"status":"incomplete","usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestOaiResponsesStreamHandler_UsesProviderUsageOnCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.output_text.delta","delta":"completed output"}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18,"input_tokens_details":{"cached_tokens":2}}}}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 7, usage.CompletionTokens)
	require.Equal(t, 18, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.CachedTokens)
}

func TestOaiResponsesStreamHandler_AllowsFallbackOnlyAfterCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.output_text.delta","delta":"completed without usage"}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Greater(t, usage.CompletionTokens, 0)
}

func TestOaiResponsesStreamHandler_AllowsEOFAfterCompletedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t,
		"data: "+`{"type":"response.output_text.delta","delta":"completed before eof"}`+"\n"+
			"data: "+`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}}`+"\n",
	)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 9, usage.TotalTokens)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
}

func TestOaiResponsesToChatStreamHandler_RequiresCompletedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"response.output_text.delta","delta":"partial output"}`,
	))
	info.RelayFormat = relaytypes.RelayFormatOpenAI

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestResponsesStreamErrorIncludesBadGatewayStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _, resp, info := newResponsesStreamTestContext(t, responsesSSE(
		`{"type":"error","error":{"type":"server_error","message":"gateway failed","code":"502"}}`,
	))

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, "gateway failed", err.ToOpenAIError().Message)
}
