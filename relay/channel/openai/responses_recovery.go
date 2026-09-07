package openai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type recoveryFrame struct {
	event, data string
	err         error
}

// Only the reader goroutine touches the upstream body. Event interpretation,
// status, writes and timers are owned by the calling goroutine, in wire order.
func readRecoveryFrames(ctx context.Context, body io.Reader, frames chan<- recoveryFrame) {
	defer close(frames)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var event string
	var data []string
	size := 0
	send := func(f recoveryFrame) bool {
		select {
		case frames <- f:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !send(recoveryFrame{event: event, data: strings.Join(data, "\n")}) {
				return
			}
			event, data, size = "", nil, 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			if !send(recoveryFrame{}) {
				return
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			size += len(value)
			if size > 8<<20 {
				send(recoveryFrame{err: fmt.Errorf("responses event exceeds 8 MiB")})
				return
			}
			data = append(data, value)
		}
	}
	if len(data) > 0 {
		if !send(recoveryFrame{event: event, data: strings.Join(data, "\n")}) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		send(recoveryFrame{err: err})
	} else {
		send(recoveryFrame{err: io.EOF})
	}
}

func recoveryError(data, eventType string) (*types.NewAPIError, bool) {
	root := gjson.Parse(data)
	status := root.Get("response.status").String()
	errorValue := root.Get("error")
	if !errorValue.Exists() || errorValue.Type == gjson.Null {
		errorValue = root.Get("response.error")
	}
	isFailure := eventType == "error" || eventType == "response.error" || eventType == "response.failed" || status == "failed"
	if errorValue.Exists() && errorValue.Type != gjson.Null {
		isFailure = true
	}
	incomplete := eventType == "response.incomplete" || status == "incomplete"
	cancelled := eventType == "response.cancelled" || eventType == "response.canceled" || status == "cancelled" || status == "canceled"
	if !isFailure && !incomplete && !cancelled {
		return nil, false
	}
	e := types.OpenAIError{Type: "upstream_error", Code: "responses_stream_failed", Message: "Upstream Responses stream failed"}
	if errorValue.Exists() && errorValue.Type != gjson.Null {
		var field any
		if common.UnmarshalJsonStr(errorValue.Raw, &field) == nil {
			if parsed := dto.GetOpenAIError(field); parsed != nil {
				e = *parsed
			}
		}
	} else if eventType == "error" || eventType == "response.error" {
		if v := root.Get("message").String(); v != "" {
			e.Message = v
		}
		if v := root.Get("code").String(); v != "" {
			e.Code = v
		}
	}
	if e.Message == "" {
		e.Message = "Upstream Responses stream failed"
	}
	code := strings.ToLower(fmt.Sprint(e.Code))
	statusCode, penalize := http.StatusBadGateway, true
	switch {
	case incomplete:
		reason := root.Get("response.incomplete_details.reason").String()
		e.Code = "responses_incomplete"
		e.Message = "Response incomplete: " + reason
		statusCode, penalize = http.StatusBadRequest, false
	case cancelled:
		e.Code = "responses_cancelled"
		e.Message = "Upstream response was cancelled"
		statusCode, penalize = http.StatusBadRequest, false
	case e.Type == "invalid_request_error" || strings.Contains(code, "context_length") || code == "content_filter" || code == "invalid_prompt":
		statusCode, penalize = http.StatusBadRequest, false
	case e.Type == "rate_limit_error" || code == "rate_limit_exceeded" || code == "429":
		statusCode = http.StatusTooManyRequests
	case e.Type == "service_unavailable_error" || code == "model_at_capacity" || code == "server_error":
		statusCode = http.StatusServiceUnavailable
	case e.Type == "authentication_error":
		statusCode = http.StatusUnauthorized
	case e.Type == "permission_error":
		statusCode = http.StatusForbidden
	}
	if !penalize {
		return types.WithOpenAIError(e, statusCode, types.ErrOptionWithSkipRetry()), false
	}
	return types.WithOpenAIError(e, statusCode), true
}

func recoveryUsage(r *dto.OpenAIResponsesResponse, u *dto.Usage) {
	if r == nil || r.Usage == nil {
		return
	}
	*u = *r.Usage
	u.PromptTokens = r.Usage.InputTokens
	u.CompletionTokens = r.Usage.OutputTokens
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	if r.Usage.InputTokensDetails != nil {
		u.PromptTokensDetails = *r.Usage.InputTokensDetails
	}
}

// responsesStructuralPreamble reports whether an event only describes the
// structure/lifecycle of a response and can therefore remain buffered before
// the first client-consumable output. Some upstreams emit output-item and
// content-part placeholders before failing; releasing those placeholders used
// to mark the stream committed even though no text, reasoning, refusal, image,
// audio or tool arguments had reached the client.
func responsesStructuralPreamble(data, eventType string, usage *dto.Usage) bool {
	if usage != nil && usage.TotalTokens > 0 {
		return false
	}
	root := gjson.Parse(data)
	switch eventType {
	case "response.created", "response.queued", "response.in_progress":
		for _, item := range root.Get("response.output").Array() {
			if responsesItemHasConsumableOutput(item) {
				return false
			}
		}
		return true
	case dto.ResponsesOutputTypeItemAdded:
		return !responsesItemHasConsumableOutput(root.Get("item"))
	case "response.content_part.added", "response.reasoning_summary_part.added":
		part := root.Get("part")
		return part.Get("text").String() == "" && part.Get("refusal").String() == "" && !part.Get("audio").Exists() && part.Get("image_url").String() == ""
	default:
		return false
	}
}

func responsesItemHasConsumableOutput(item gjson.Result) bool {
	if item.Get("arguments").String() != "" || item.Get("result").String() != "" || item.Get("encrypted_content").String() != "" {
		return true
	}
	for _, path := range []string{"content", "summary"} {
		for _, part := range item.Get(path).Array() {
			if part.Get("text").String() != "" || part.Get("refusal").String() != "" || part.Get("audio").Exists() || part.Get("image_url").String() != "" {
				return true
			}
		}
	}
	return false
}

func responsesEventHasConsumableOutput(data string, usage *dto.Usage) bool {
	if usage != nil && usage.TotalTokens > 0 {
		return true
	}
	root := gjson.Parse(data)
	for _, path := range []string{"delta", "text", "refusal", "arguments", "result", "encrypted_content", "partial_image_b64", "audio", "image_url"} {
		if value := root.Get(path); value.Exists() && value.String() != "" {
			return true
		}
	}
	if responsesItemHasConsumableOutput(root.Get("item")) {
		return true
	}
	for _, item := range root.Get("output").Array() {
		if responsesItemHasConsumableOutput(item) {
			return true
		}
	}
	for _, item := range root.Get("response.output").Array() {
		if responsesItemHasConsumableOutput(item) {
			return true
		}
	}
	if responsesItemHasConsumableOutput(root.Get("response")) {
		return true
	}
	if part := root.Get("part"); part.Exists() {
		return part.Get("text").String() != "" || part.Get("refusal").String() != "" || part.Get("audio").Exists() || part.Get("image_url").String() != ""
	}
	return false
}

func responsesRecoveryStream(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	usage := &dto.Usage{}
	var outputText strings.Builder
	outcome := &relaycommon.ResponsesRecoveryOutcome{}
	info.ResponsesRecovery = outcome
	info.StreamStatus = relaycommon.NewStreamStatus()
	finish := func(err *types.NewAPIError, reason relaycommon.StreamEndReason, penalize bool) (*dto.Usage, *types.NewAPIError) {
		if err != nil {
			stateful := false
			if request, ok := info.Request.(*dto.OpenAIResponsesRequest); ok && request.PreviousResponseID != "" {
				err = types.WithOpenAIError(err.ToOpenAIError(), err.StatusCode, types.ErrOptionWithSkipRetry())
				stateful = true
			}
			// Once a semantic event is released, the client must receive the real
			// upstream error but this request must never be replayed on another
			// channel. Preserve that decision on the recorded outcome as well.
			if outcome.Committed && !stateful {
				err = types.WithOpenAIError(err.ToOpenAIError(), err.StatusCode, types.ErrOptionWithSkipRetry())
			}
			info.StreamStatus.RecordError(err.MaskSensitiveError())
			c.Set("responses_recovery_failed", true)
		}
		outcome.Error, outcome.Penalize = err, penalize
		var endErr error
		if err != nil {
			endErr = err
		}
		info.StreamStatus.SetEndReason(reason, endErr)
		logger.LogInfo(c, fmt.Sprintf("responses recovery: committed=%t commit_event=%q %s", outcome.Committed, outcome.CommitEvent, info.StreamStatus.Summary()))
		if err != nil && !outcome.Committed {
			return nil, err
		}
		return usage, nil
	}
	if resp == nil || resp.Body == nil {
		return finish(types.NewOpenAIError(fmt.Errorf("empty upstream response"), types.ErrorCodeEmptyResponse, 502), "responses_failed", true)
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	frames := make(chan recoveryFrame)
	done := make(chan struct{})
	go func() { defer close(done); readRecoveryFrames(ctx, resp.Body, frames) }()
	defer func() { cancel(); resp.Body.Close(); <-done }()
	idleSeconds := constant.StreamingTimeout
	if idleSeconds <= 0 {
		idleSeconds = 300
	}
	idle := time.NewTimer(time.Duration(idleSeconds) * time.Second)
	defer idle.Stop()
	preSeconds := common.GetEnvOrDefault("RESPONSES_RECOVERY_PREOUTPUT_SECONDS", 90)
	if preSeconds < 1 {
		preSeconds = 1
	}
	if preSeconds > 300 {
		preSeconds = 300
	}
	pre := time.NewTimer(time.Duration(preSeconds) * time.Second)
	defer pre.Stop()
	preC := pre.C
	settings := operation_setting.GetGeneralSetting()
	pingSeconds := settings.PingIntervalSeconds
	if pingSeconds <= 0 {
		pingSeconds = 10
	}
	ping := time.NewTicker(time.Duration(pingSeconds) * time.Second)
	defer ping.Stop()
	var pending []recoveryFrame
	pendingBytes := 0
	pendingDiagnosticLogged := false
	sequence := int64(-1)
	commit := func(eventType string) {
		if !outcome.Committed {
			outcome.Committed = true
			outcome.CommitEvent = eventType
		}
	}
	write := func(f recoveryFrame) error {
		if c.Request.Context().Err() != nil {
			return c.Request.Context().Err()
		}
		helper.SetEventStreamHeaders(c)
		commit(f.event)
		if f.event != "" {
			if _, err := fmt.Fprintf(c.Writer, "event: %s\n", f.event); err != nil {
				return err
			}
		}
		for _, line := range strings.Split(f.data, "\n") {
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n", line); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(c.Writer, "\n"); err != nil {
			return err
		}
		return helper.FlushWriter(c)
	}
	flush := func(commitEvent string) error {
		preC = nil
		pre.Stop()
		commit(commitEvent)
		for _, f := range pending {
			if err := write(f); err != nil {
				return err
			}
		}
		pending = nil
		pendingBytes = 0
		return nil
	}
	fail := func(err *types.NewAPIError, reason relaycommon.StreamEndReason, penalize bool, original *recoveryFrame) (*dto.Usage, *types.NewAPIError) {
		if outcome.Committed && c.Request.Context().Err() == nil {
			if original != nil {
				_ = write(*original)
			} else {
				payload, _ := common.Marshal(map[string]any{"type": "error", "error": err.ToOpenAIError(), "sequence_number": sequence + 1})
				_ = write(recoveryFrame{event: "error", data: string(payload)})
			}
		}
		return finish(err, reason, penalize)
	}
	clientGone := func() (*dto.Usage, *types.NewAPIError) {
		return finish(types.NewOpenAIError(fmt.Errorf("client disconnected"), types.ErrorCodeBadResponse, 499, types.ErrOptionWithSkipRetry()), relaycommon.StreamEndReasonClientGone, false)
	}
	for {
		if ctx.Err() != nil {
			return clientGone()
		}
		select {
		case <-ctx.Done():
			return clientGone()
		case <-preC:
			return fail(types.NewOpenAIError(fmt.Errorf("upstream produced no consumable Responses event before deadline"), types.ErrorCodeEmptyResponse, 504), relaycommon.StreamEndReasonTimeout, true, nil)
		case <-idle.C:
			return fail(types.NewOpenAIError(fmt.Errorf("upstream Responses stream idle timeout"), types.ErrorCodeBadResponse, 504), relaycommon.StreamEndReasonTimeout, true, nil)
		case <-ping.C:
			if outcome.Committed && settings.PingIntervalEnabled && !info.DisablePing {
				if helper.PingData(c) != nil {
					return clientGone()
				}
			}
		case f, ok := <-frames:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(time.Duration(idleSeconds) * time.Second)
			if !ok || f.err != nil {
				if ctx.Err() != nil {
					return clientGone()
				}
				return fail(types.NewOpenAIError(fmt.Errorf("upstream Responses stream ended without a terminal event"), types.ErrorCodeBadResponse, 502), "responses_truncated", true, nil)
			}
			if f.data == "" {
				continue
			}
			if f.data == "[DONE]" {
				return fail(types.NewOpenAIError(fmt.Errorf("Responses [DONE] arrived without a terminal response"), types.ErrorCodeBadResponse, 502), "responses_truncated", true, nil)
			}
			var event dto.ResponsesStreamResponse
			if err := common.UnmarshalJsonStr(f.data, &event); err != nil {
				return fail(types.NewOpenAIError(fmt.Errorf("invalid Responses event JSON"), types.ErrorCodeBadResponse, 502), "responses_invalid_event", true, nil)
			}
			if event.Type == "" {
				event.Type = f.event
			}
			if f.event == "" {
				f.event = event.Type
			}
			if n := gjson.Get(f.data, "sequence_number"); n.Exists() {
				sequence = n.Int()
			}
			info.ReceivedResponseCount++
			recoveryUsage(event.Response, usage)
			if err, penalize := recoveryError(f.data, event.Type); err != nil {
				// Usage means upstream work occurred: preserve billing, do not replay.
				if !outcome.Committed && usage.TotalTokens > 0 {
					if flush("upstream_usage") != nil {
						return clientGone()
					}
				}
				return fail(err, "responses_failed", penalize, &f)
			}
			terminal := event.Type == "response.completed" || event.Type == "response.done"
			if terminal && (event.Response == nil || gjson.Get(f.data, "response.status").String() != "completed") {
				return fail(types.NewOpenAIError(fmt.Errorf("invalid Responses terminal status"), types.ErrorCodeBadResponse, 502), "responses_invalid_event", true, nil)
			}
			isPreamble := responsesStructuralPreamble(f.data, event.Type, usage) || (!terminal && !responsesEventHasConsumableOutput(f.data, usage))
			prospectiveFrames := len(pending) + 1
			prospectiveBytes := pendingBytes + len(f.data)
			if !outcome.Committed && isPreamble && !pendingDiagnosticLogged && (prospectiveFrames > 10 || prospectiveBytes > 128<<10) {
				pendingDiagnosticLogged = true
				logger.LogInfo(c, fmt.Sprintf("responses recovery: buffering structural preamble frames=%d bytes=%d", prospectiveFrames, prospectiveBytes))
			}
			if !outcome.Committed && isPreamble && prospectiveBytes <= 256<<10 {
				pending = append(pending, f)
				pendingBytes = prospectiveBytes
				continue
			}
			if flush(event.Type) != nil {
				return clientGone()
			}
			info.SetFirstResponseTime()
			if write(f) != nil {
				return clientGone()
			}
			if event.Type == "response.output_text.delta" {
				outputText.WriteString(event.Delta)
			}
			if event.Type == dto.ResponsesOutputTypeItemDone && event.Item != nil && event.Item.Type == dto.BuildInCallWebSearchCall && info.ResponsesUsageInfo != nil {
				if tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; tool != nil {
					tool.CallCount++
				}
			}
			if terminal {
				if event.Response.Usage == nil && outputText.Len() > 0 {
					usage.CompletionTokens = service.CountTextToken(outputText.String(), info.UpstreamModelName)
					usage.PromptTokens = info.GetEstimatePromptTokens()
					usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				}
				if event.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", event.Response.GetQuality())
					c.Set("image_generation_call_size", event.Response.GetSize())
				}
				return finish(nil, relaycommon.StreamEndReasonDone, false)
			}
		}
	}
}
