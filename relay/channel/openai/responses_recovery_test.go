package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const recoveryCreated = "data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"status\":\"in_progress\",\"output\":[]}}\n\n"
const recoveryStructuralMetadata = "data: {\"type\":\"response.in_progress\",\"sequence_number\":1,\"response\":{\"status\":\"in_progress\",\"output\":[]}}\n\n" +
	"data: {\"type\":\"response.output_item.added\",\"sequence_number\":2,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"content\":[]}}\n\n" +
	"data: {\"type\":\"response.reasoning_summary_part.added\",\"sequence_number\":3,\"part\":{\"type\":\"summary_text\",\"text\":\"\"}}\n\n" +
	"data: {\"type\":\"response.content_part.added\",\"sequence_number\":4,\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n\n"
const recoveryDelta = "data: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"delta\":\"hello\"}\n\n"
const recoveryReasoningDelta = "data: {\"type\":\"response.reasoning_summary_text.delta\",\"sequence_number\":4,\"delta\":\"thinking\"}\n\n"
const recoveryOverload = "event: error\ndata: {\"error\":{\"code\":\"server_error\",\"message\":\"Our servers are currently overloaded. Please try again later.\",\"param\":null,\"type\":\"service_unavailable_error\"},\"sequence_number\":4,\"type\":\"error\"}\n\n"
const recoveryCompleted = "data: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n"

func runRecovery(body io.ReadCloser, ctx context.Context, previous string) (*httptest.ResponseRecorder, *relaycommon.RelayInfo, *dto.Usage, *types.NewAPIError) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{Request: &dto.OpenAIResponsesRequest{PreviousResponseID: previous}, StartTime: time.Now()}
	usage, err := responsesRecoveryStream(c, info, &http.Response{StatusCode: 200, Body: body})
	return w, info, usage, err
}

func TestResponsesRecoveryEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, body         string
		status             int
		committed, penalty bool
		tokens             int
	}{
		{"incident_preoutput", recoveryCreated + recoveryOverload, 503, false, true, 0},
		{"created_with_output_placeholder_then_overload", "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"content\":[]}]}}\n\n" + recoveryOverload, 503, false, true, 0},
		{"created_with_text_then_overload", "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}]}}\n\n" + recoveryOverload, 503, true, true, 0},
		{"structural_metadata_then_overload", recoveryCreated + recoveryStructuralMetadata + recoveryOverload, 503, false, true, 0},
		{"failed_nested", recoveryCreated + "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"overload\"}}}\n\n", 503, false, true, 0},
		{"empty_eof", "", 502, false, true, 0},
		{"preamble_eof", recoveryCreated, 502, false, true, 0},
		{"done_without_terminal", recoveryCreated + "data: [DONE]\n\n", 502, false, true, 0},
		{"malformed", recoveryCreated + "data: {bad}\n\n", 502, false, true, 0},
		{"missing_response", "data: {\"type\":\"response.completed\"}\n\n", 502, false, true, 0},
		{"wrong_terminal_status", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"in_progress\"}}\n\n", 502, false, true, 0},
		{"success", recoveryCreated + recoveryDelta + recoveryCompleted, 0, true, false, 12},
		{"output_error", recoveryCreated + recoveryDelta + recoveryOverload, 503, true, true, 0},
		{"reasoning_output_error", recoveryCreated + recoveryStructuralMetadata + recoveryReasoningDelta + recoveryOverload, 503, true, true, 0},
		{"output_eof", recoveryDelta, 502, true, true, 0},
		{"metadata_event_then_overload", "data: {\"type\":\"codex.rate_limits\",\"limits\":{\"primary\":{\"used_percent\":10}}}\n\n" + recoveryOverload, 503, false, true, 0},
		{"tool_started_without_arguments_then_overload", "data: {\"type\":\"response.tool_call.started\"}\n\n" + recoveryOverload, 503, false, true, 0},
		{"image_output_then_overload", "data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"abc\"}\n\n" + recoveryOverload, 503, true, true, 0},
		{"invalid_request", "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"bad input\"}}\n\n", 400, false, false, 0},
		{"incomplete", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n", 400, false, false, 0},
		{"failed_with_usage", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\"},\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n", 503, true, true, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, info, usage, err := runRecovery(io.NopCloser(strings.NewReader(tt.body)), context.Background(), "")
			require.Equal(t, tt.committed, info.ResponsesRecovery.Committed)
			require.Equal(t, tt.penalty, info.ResponsesRecovery.Penalize)
			if tt.status == 0 {
				require.Nil(t, err)
				require.Nil(t, info.ResponsesRecovery.Error)
				require.Contains(t, w.Body.String(), "response.completed")
			} else {
				require.NotNil(t, info.ResponsesRecovery.Error)
				require.Equal(t, tt.status, info.ResponsesRecovery.Error.StatusCode)
				if tt.committed {
					require.Nil(t, err)
					require.True(t, types.IsSkipRetryError(info.ResponsesRecovery.Error), "committed stream errors must never be replayed")
					require.True(t, strings.Contains(w.Body.String(), "event: error") || strings.Contains(w.Body.String(), "event: response.failed"))
					require.NotContains(t, w.Body.String(), "response.completed")
				} else {
					require.NotNil(t, err)
					require.Empty(t, w.Body.String())
					require.Empty(t, w.Header().Get("Content-Type"))
				}
				if !tt.penalty {
					require.True(t, types.IsSkipRetryError(info.ResponsesRecovery.Error))
				}
			}
			if usage != nil {
				require.Equal(t, tt.tokens, usage.TotalTokens)
			}
			if tt.committed {
				require.NotEmpty(t, info.ResponsesRecovery.CommitEvent)
			} else {
				require.Empty(t, info.ResponsesRecovery.CommitEvent)
			}
		})
	}
}

func TestResponsesRecoveryBuffersLargeStructuralPreamble(t *testing.T) {
	metadata := strings.Repeat("x", 200<<10)
	body := "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[{\"type\":\"message\",\"content\":[],\"metadata\":\"" + metadata + "\"}]}}\n\n" + recoveryOverload
	w, info, _, err := runRecovery(io.NopCloser(strings.NewReader(body)), context.Background(), "")
	require.NotNil(t, err)
	require.Equal(t, 503, err.StatusCode)
	require.False(t, info.ResponsesRecovery.Committed)
	require.Empty(t, w.Body.String())
}

func TestResponsesRecoveryCancellationAndStateful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, info, _, err := runRecovery(io.NopCloser(strings.NewReader(recoveryCreated)), ctx, "")
	require.Equal(t, 499, err.StatusCode)
	require.False(t, info.ResponsesRecovery.Penalize)
	require.Empty(t, w.Body.String())
	_, _, _, err = runRecovery(io.NopCloser(strings.NewReader(recoveryOverload)), context.Background(), "resp_prior")
	require.True(t, types.IsSkipRetryError(err))
}

func TestResponsesRecoveryTerminalClosesOpenStream(t *testing.T) {
	reader, writer := io.Pipe()
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, _ = io.WriteString(writer, recoveryCompleted)
		_, _ = io.WriteString(writer, "ignored")
	}()
	_, _, u, err := runRecovery(reader, context.Background(), "")
	require.Nil(t, err)
	require.Equal(t, 12, u.TotalTokens)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("upstream reader leaked")
	}
	_ = writer.Close()
}

func TestResponsesRecoveryPreoutputDeadline(t *testing.T) {
	t.Setenv("RESPONSES_RECOVERY_PREOUTPUT_SECONDS", "1")
	reader, writer := io.Pipe()
	defer writer.Close()
	w, info, _, err := runRecovery(reader, context.Background(), "")
	require.Equal(t, 504, err.StatusCode)
	require.True(t, info.ResponsesRecovery.Penalize)
	require.Empty(t, w.Body.String())
}

func TestResponsesRecoveryConcurrentStreams(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, info, u, err := runRecovery(io.NopCloser(strings.NewReader(recoveryCreated+recoveryDelta+recoveryCompleted)), context.Background(), "")
			if err != nil || u == nil || u.TotalTokens != 12 || !info.ResponsesRecovery.Committed || strings.Count(w.Body.String(), "\"delta\":\"hello\"") != 1 {
				t.Error("concurrent stream corrupted")
			}
		}()
	}
	wg.Wait()
}

func TestResponsesRecoveryMultiline(t *testing.T) {
	body := "event: response.completed\ndata: {\"type\":\"response.completed\",\ndata: \"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
	_, _, u, err := runRecovery(io.NopCloser(strings.NewReader(body)), context.Background(), "")
	require.Nil(t, err)
	require.Equal(t, 12, u.TotalTokens)
}
