package controller

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/clawx_client_setting"
	"github.com/gin-gonic/gin"
)

const (
	clawXEnvelopeMaxBytes           = int64(5 * 1024 * 1024)
	clawXEnvelopeMaxDecodedBytes    = int64(5 * 1024 * 1024)
	clawXEnvelopeMaxSanitizedBytes  = int64(256 * 1024)
	clawXEnvelopeMaxHeaderBytes     = 16 * 1024
	clawXEnvelopeMaxItemHeaderBytes = 8 * 1024
	clawXEnvelopeMaxEventBytes      = 1024 * 1024
	clawXEnvelopeMaxItems           = 100
	clawXEnvelopeMaxEvents          = 20
	clawXEnvelopeIPLimit            = 120
	clawXEnvelopeIPWindow           = time.Minute
	clawXEnvelopeInstallWindow      = time.Hour
	clawXEnvelopeUpstreamTimeout    = 15 * time.Second
)

const clawXRedisRateLimitScript = `
local limit = tonumber(ARGV[1])
local amount = tonumber(ARGV[3])
local current_raw = redis.call('GET', KEYS[1])
local current = 0
if current_raw then
  current = tonumber(current_raw)
  if current == nil or current < 0 or current ~= math.floor(current) then
    return -1
  end
end
if limit == nil or amount == nil or limit < 1 or amount < 1 or current + amount > limit then
  return 0
end
local count = redis.call('INCRBY', KEYS[1], amount)
if count == amount then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 1
`

var clawXInstallIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var clawXUUIDRequestIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var clawXUClawRequestIDPattern = regexp.MustCompile(`(?i)^uclaw-(?:request|req|sentry)-[a-z0-9]{4,64}$`)
var clawXEventIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
var clawXSafeDiagnosticNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.<>-]{0,127}$`)
var clawXSafeStagePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var clawXSpanIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{16}$`)
var clawXDiagnosticHashPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var clawXSensitiveDiagnosticPattern = regexp.MustCompile(`(?i)(?:authorization|cookie|credential|password|passwd|passphrase|secret|token|api[_-]?key|prompt|instruction|raw[_-]?params|file[_-]?content|sk-[a-z0-9_-]{8,}|gh[pousr]_[a-z0-9]{8,}|xox[baprs]-[a-z0-9-]{8,}|AIza[a-z0-9_-]{20,}|AKIA[0-9A-Z]{12,}|eyJ[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,})`)

var clawXBlockedSentryPrefixes = []netip.Prefix{
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fec0::/10"),
}

type clawXSlidingWindowLimiter struct {
	mu        sync.Mutex
	events    map[string][]time.Time
	lastSweep time.Time
}

var clawXObservabilityLimiter = &clawXSlidingWindowLimiter{events: map[string][]time.Time{}}
var clawXRedisRateLimitEval = func(ctx context.Context, key string, limit, amount int, window time.Duration) (int, error) {
	return common.RDB.Eval(
		ctx,
		clawXRedisRateLimitScript,
		[]string{clawXRedisRateLimitKey(key)},
		limit,
		window.Milliseconds(),
		amount,
	).Int()
}
var clawXSentryLookupIP = net.DefaultResolver.LookupIPAddr
var clawXSentryHTTPClient = &http.Client{
	Timeout:   clawXEnvelopeUpstreamTimeout,
	Transport: clawXSentryTransport(),
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func clawXSentryTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	} else {
		base = base.Clone()
	}
	base.Proxy = nil
	base.MaxResponseHeaderBytes = 64 * 1024
	base.DialTLS = nil
	base.DialTLSContext = nil
	dialer := &net.Dialer{Timeout: clawXEnvelopeUpstreamTimeout}
	base.DialContext = clawXSentryDialContext(dialer)
	return base
}

func clawXSentryDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid Sentry target address: %w", err)
		}

		ips := make([]net.IP, 0, 4)
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
		} else {
			resolved, resolveErr := clawXSentryLookupIP(ctx, host)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve Sentry target: %w", resolveErr)
			}
			for _, address := range resolved {
				if address.Zone != "" || address.IP == nil {
					return nil, errors.New("Sentry target resolved to an invalid address")
				}
				ips = append(ips, address.IP)
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("Sentry target did not resolve to an address")
		}
		for _, ip := range ips {
			if clawXBlockedSentryIP(ip) {
				return nil, errors.New("Sentry target resolved to a blocked address")
			}
		}

		var lastErr error
		for _, ip := range ips {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				remoteHost, _, splitErr := net.SplitHostPort(conn.RemoteAddr().String())
				remoteIP := net.ParseIP(remoteHost)
				if splitErr != nil || remoteIP == nil || clawXBlockedSentryIP(remoteIP) || !remoteIP.Equal(ip) {
					_ = conn.Close()
					return nil, errors.New("Sentry target connected to an unexpected address")
				}
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = errors.New("Sentry target connection failed")
		}
		return nil, lastErr
	}
}

var errClawXEnvelopeTooLarge = errors.New("envelope_too_large")

var clawXIsSafeSentryTarget = clawXDefaultIsSafeSentryTarget

func clawXDefaultIsSafeSentryTarget(target *url.URL) bool {
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !clawXBlockedSentryIP(ip) && clawXTrustedDomain(host)
	}
	if !clawXTrustedDomain(host) {
		return false
	}
	return true
}

func clawXTrustedDomain(host string) bool {
	for _, raw := range strings.Split(os.Getenv("CLAWX_SENTRY_TRUSTED_DOMAINS"), ",") {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func clawXBlockedSentryIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	address, err := netip.ParseAddr(ip.String())
	if err != nil {
		return true
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range clawXBlockedSentryPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func clawXValidateSentryTarget(target *url.URL) error {
	if !clawXIsSafeSentryTarget(target) {
		return errors.New("Sentry target is not an allowed public endpoint")
	}
	return nil
}

func clawXTrustedProxyClientIP(request *http.Request) string {
	return middleware.ClawXTrustedProxyClientIP(request)
}

func (limiter *clawXSlidingWindowLimiter) allow(key string, limit, amount int, window time.Duration, now time.Time) bool {
	if limit < 1 || amount < 1 || amount > limit {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.lastSweep.IsZero() || now.Sub(limiter.lastSweep) >= time.Minute {
		oldestUseful := now.Add(-clawXEnvelopeInstallWindow)
		for eventKey, events := range limiter.events {
			firstUseful := 0
			for firstUseful < len(events) && events[firstUseful].Before(oldestUseful) {
				firstUseful++
			}
			if firstUseful == len(events) {
				delete(limiter.events, eventKey)
			} else if firstUseful > 0 {
				limiter.events[eventKey] = events[firstUseful:]
			}
		}
		limiter.lastSweep = now
	}
	cutoff := now.Add(-window)
	existing := limiter.events[key]
	first := 0
	for first < len(existing) && existing[first].Before(cutoff) {
		first++
	}
	existing = existing[first:]
	if len(existing)+amount > limit {
		limiter.events[key] = existing
		return false
	}
	for index := 0; index < amount; index++ {
		existing = append(existing, now)
	}
	limiter.events[key] = existing
	return true
}

func clawXRedisRateLimitKey(key string) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("rateLimit:CXO:%x", digest)
}

func allowClawXObservabilityEvent(ctx context.Context, key string, limit, amount int, window time.Duration, now time.Time) (allowed bool, processLocal bool, redisFailure bool) {
	if limit < 1 || amount < 1 || amount > limit {
		return false, false, false
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return false, false, true
		}
		result, err := clawXRedisRateLimitEval(ctx, key, limit, amount, window)
		if err == nil {
			switch result {
			case 1:
				return true, false, false
			case 0:
				return false, false, false
			default:
				return false, false, true
			}
		}
		return false, false, true
	}
	return clawXObservabilityLimiter.allow(key, limit, amount, window, now), true, false
}

type clawXEnvelopeCorrelation struct {
	RequestID  string
	EventID    string
	TraceID    string
	EventCount int
}

func clawXReadLimited(reader io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("content exceeds limit")
	}
	return content, nil
}

func clawXDecodeEnvelopeBody(body []byte, contentEncoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("invalid gzip envelope")
		}
		defer reader.Close()
		return clawXReadLimited(reader, clawXEnvelopeMaxDecodedBytes)
	default:
		return nil, fmt.Errorf("unsupported envelope encoding")
	}
}

func clawXEncodeEnvelopeBody(body []byte, contentEncoding string) ([]byte, error) {
	if strings.EqualFold(strings.TrimSpace(contentEncoding), "") {
		return body, nil
	}
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func clawXEnvelopeLine(body []byte, offset *int) ([]byte, bool) {
	if *offset >= len(body) {
		return nil, false
	}
	remaining := body[*offset:]
	newline := bytes.IndexByte(remaining, '\n')
	if newline < 0 {
		*offset = len(body)
		return remaining, true
	}
	line := remaining[:newline]
	*offset += newline + 1
	return line, true
}

func clawXEnvelopeItemLength(header map[string]any) (int, bool, error) {
	length, exists := header["length"]
	if !exists {
		return 0, false, nil
	}
	value, ok := length.(float64)
	if !ok || value < 0 || value != float64(int(value)) || value > float64(clawXEnvelopeMaxDecodedBytes) {
		return 0, false, fmt.Errorf("invalid envelope item length")
	}
	return int(value), true, nil
}

func clawXEnvelopeID(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), "-", ""))
	if !clawXEventIDPattern.MatchString(text) {
		return ""
	}
	return text
}

func clawXCorrelationID(requestID, installID, correlationSeed string) string {
	requestID = strings.TrimSpace(requestID)
	if clawXUUIDRequestIDPattern.MatchString(requestID) || clawXUClawRequestIDPattern.MatchString(requestID) {
		return requestID
	}
	digest := sha256.Sum256([]byte(installID + ":" + correlationSeed))
	return "uclaw-sentry-" + hex.EncodeToString(digest[:12])
}

func clawXAttachCorrelationTags(payload map[string]any, requestID string) {
	tags, ok := payload["tags"].(map[string]any)
	if !ok {
		tags = map[string]any{}
		payload["tags"] = tags
	}
	tags["uclaw_request_id"] = requestID
}

func clawXTraceID(payload map[string]any) string {
	contexts, ok := payload["contexts"].(map[string]any)
	if !ok {
		return ""
	}
	trace, ok := contexts["trace"].(map[string]any)
	if !ok {
		return ""
	}
	return clawXEnvelopeID(trace["trace_id"])
}

func clawXSafeDiagnosticString(value any) string {
	text, ok := value.(string)
	if !ok || !clawXSafeDiagnosticNamePattern.MatchString(text) || clawXSensitiveDiagnosticPattern.MatchString(text) || strings.ContainsAny(text, "\r\n\x00") {
		return ""
	}
	return text
}

func clawXSafeDiagnosticNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number > 1_000_000_000_000 {
		return 0, false
	}
	return number, true
}

func clawXProjectStackFrame(value any) map[string]any {
	frame, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	projected := map[string]any{}
	if filename, ok := frame["filename"].(string); ok {
		filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
		if clawXSafeDiagnosticString(filename) != "" {
			projected["filename"] = filename
		}
	}
	if function := clawXSafeDiagnosticString(frame["function"]); function != "" {
		projected["function"] = function
	}
	for _, key := range []string{"lineno", "colno"} {
		if number, ok := clawXSafeDiagnosticNumber(frame[key]); ok {
			projected[key] = number
		}
	}
	if inApp, ok := frame["in_app"].(bool); ok {
		projected["in_app"] = inApp
	}
	if len(projected) == 0 {
		return nil
	}
	return projected
}

func clawXProjectException(value any) map[string]any {
	exception, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	values, ok := exception["values"].([]any)
	if !ok {
		return nil
	}
	projectedValues := make([]any, 0, min(len(values), 10))
	for _, value := range values {
		if len(projectedValues) == 10 {
			break
		}
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		projected := map[string]any{}
		if exceptionType := clawXSafeDiagnosticString(entry["type"]); exceptionType != "" {
			projected["type"] = exceptionType
		}
		if mechanism, ok := entry["mechanism"].(map[string]any); ok {
			projectedMechanism := map[string]any{}
			if mechanismType := clawXSafeDiagnosticString(mechanism["type"]); mechanismType != "" {
				projectedMechanism["type"] = mechanismType
			}
			if handled, ok := mechanism["handled"].(bool); ok {
				projectedMechanism["handled"] = handled
			}
			if len(projectedMechanism) > 0 {
				projected["mechanism"] = projectedMechanism
			}
		}
		if stacktrace, ok := entry["stacktrace"].(map[string]any); ok {
			if frames, ok := stacktrace["frames"].([]any); ok {
				projectedFrames := make([]any, 0, min(len(frames), 20))
				for _, frame := range frames {
					if len(projectedFrames) == 20 {
						break
					}
					if projectedFrame := clawXProjectStackFrame(frame); projectedFrame != nil {
						projectedFrames = append(projectedFrames, projectedFrame)
					}
				}
				if len(projectedFrames) > 0 {
					projected["stacktrace"] = map[string]any{"frames": projectedFrames}
				}
			}
		}
		if len(projected) > 0 {
			projectedValues = append(projectedValues, projected)
		}
	}
	if len(projectedValues) == 0 {
		return nil
	}
	return map[string]any{"values": projectedValues}
}

func clawXProjectTrace(value any) map[string]any {
	trace, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	projected := map[string]any{}
	if traceID := clawXEnvelopeID(trace["trace_id"]); traceID != "" {
		projected["trace_id"] = traceID
	}
	if spanID, ok := trace["span_id"].(string); ok && clawXSpanIDPattern.MatchString(spanID) {
		projected["span_id"] = strings.ToLower(spanID)
	}
	if status, ok := trace["status"].(string); ok && clawXSafeStagePattern.MatchString(status) && clawXSafeDiagnosticString(status) != "" {
		projected["status"] = status
	}
	if len(projected) == 0 {
		return nil
	}
	return projected
}

func clawXProjectDiagnosticTags(value any) map[string]any {
	tags, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	projected := map[string]any{}
	for _, key := range []string{"stage", "status", "component", "operation", "error_code"} {
		if text, ok := tags[key].(string); ok && clawXSafeStagePattern.MatchString(text) && !clawXSensitiveDiagnosticPattern.MatchString(text) {
			projected[key] = text
		}
	}
	if len(projected) == 0 {
		return nil
	}
	return projected
}

func clawXProjectDiagnosticExtra(value any) map[string]any {
	extra, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	projected := map[string]any{}
	for _, key := range []string{"payload_hash", "request_hash", "response_hash", "content_hash"} {
		if hash, ok := extra[key].(string); ok && clawXDiagnosticHashPattern.MatchString(hash) {
			projected[key] = strings.ToLower(hash)
		}
	}
	for _, key := range []string{"input_length", "output_length", "request_length", "response_length", "duration_ms", "attempt"} {
		if number, ok := clawXSafeDiagnosticNumber(extra[key]); ok {
			projected[key] = number
		}
	}
	if len(projected) == 0 {
		return nil
	}
	return projected
}

func clawXProjectEnvelopePayload(itemType string, payload map[string]any, requestID string) map[string]any {
	projected := map[string]any{}
	if eventID := clawXEnvelopeID(payload["event_id"]); eventID != "" {
		projected["event_id"] = eventID
	}
	if level, ok := payload["level"].(string); ok {
		switch level {
		case "fatal", "error", "warning", "info", "debug":
			projected["level"] = level
		}
	}
	if timestamp, ok := clawXSafeDiagnosticNumber(payload["timestamp"]); ok {
		projected["timestamp"] = timestamp
	}
	if contexts, ok := payload["contexts"].(map[string]any); ok {
		if trace := clawXProjectTrace(contexts["trace"]); trace != nil {
			projected["contexts"] = map[string]any{"trace": trace}
		}
	}
	if tags := clawXProjectDiagnosticTags(payload["tags"]); tags != nil {
		projected["tags"] = tags
	}
	if extra := clawXProjectDiagnosticExtra(payload["extra"]); extra != nil {
		projected["extra"] = extra
	}
	if itemType == "event" {
		if exception := clawXProjectException(payload["exception"]); exception != nil {
			projected["exception"] = exception
		}
	}
	clawXAttachCorrelationTags(projected, requestID)
	return projected
}

func clawXEnvelopeHeaderProjection(header map[string]any) map[string]any {
	projected := map[string]any{}
	if eventID := clawXEnvelopeID(header["event_id"]); eventID != "" {
		projected["event_id"] = eventID
	}
	return projected
}

func clawXSanitizeEnvelope(body []byte, installID, requestID string) ([]byte, clawXEnvelopeCorrelation, error) {
	offset := 0
	headerLine, ok := clawXEnvelopeLine(body, &offset)
	if !ok || len(headerLine) == 0 {
		return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("missing envelope header")
	}
	if len(headerLine) > clawXEnvelopeMaxHeaderBytes {
		return nil, clawXEnvelopeCorrelation{}, errClawXEnvelopeTooLarge
	}
	var envelopeHeader map[string]any
	if err := common.Unmarshal(headerLine, &envelopeHeader); err != nil {
		return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope header")
	}
	if envelopeHeader == nil {
		return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope header")
	}
	delete(envelopeHeader, "dsn")
	eventID := clawXEnvelopeID(envelopeHeader["event_id"])
	correlationSeed := eventID
	if correlationSeed == "" {
		digest := sha256.Sum256(body)
		correlationSeed = hex.EncodeToString(digest[:])
	}
	correlationID := clawXCorrelationID(requestID, installID, correlationSeed)
	var sanitized bytes.Buffer
	encodedHeader, err := common.Marshal(clawXEnvelopeHeaderProjection(envelopeHeader))
	if err != nil {
		return nil, clawXEnvelopeCorrelation{}, err
	}
	sanitized.Write(encodedHeader)
	sanitized.WriteByte('\n')
	traceID := ""
	forwardedItems := 0
	processedItems := 0
	processedEvents := 0
	for offset < len(body) {
		itemLine, exists := clawXEnvelopeLine(body, &offset)
		if !exists || len(itemLine) == 0 {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope item header")
		}
		processedItems++
		if processedItems > clawXEnvelopeMaxItems || len(itemLine) > clawXEnvelopeMaxItemHeaderBytes {
			return nil, clawXEnvelopeCorrelation{}, errClawXEnvelopeTooLarge
		}
		var itemHeader map[string]any
		if err := common.Unmarshal(itemLine, &itemHeader); err != nil {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope item header")
		}
		if itemHeader == nil {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope item header")
		}
		itemTypeValue, typeOK := itemHeader["type"].(string)
		if !typeOK || strings.TrimSpace(itemTypeValue) == "" {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("envelope item type is required")
		}
		itemType := strings.ToLower(strings.TrimSpace(itemTypeValue))
		payloadLength, hasLength, err := clawXEnvelopeItemLength(itemHeader)
		if err != nil {
			return nil, clawXEnvelopeCorrelation{}, err
		}
		var payload []byte
		if hasLength {
			if offset+payloadLength > len(body) {
				return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("truncated envelope item")
			}
			payload = body[offset : offset+payloadLength]
			offset += payloadLength
			if offset < len(body) {
				if body[offset] != '\n' {
					return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid envelope item length delimiter")
				}
				offset++
			}
		} else {
			var payloadExists bool
			payload, payloadExists = clawXEnvelopeLine(body, &offset)
			if !payloadExists {
				return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("missing envelope item payload")
			}
		}
		if itemType != "event" && itemType != "transaction" {
			continue
		}
		if len(payload) > clawXEnvelopeMaxEventBytes {
			return nil, clawXEnvelopeCorrelation{}, errClawXEnvelopeTooLarge
		}
		var payloadMap map[string]any
		if err := common.Unmarshal(payload, &payloadMap); err != nil {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid JSON envelope item")
		}
		if payloadMap == nil {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("invalid JSON envelope item")
		}
		processedEvents++
		if processedEvents > clawXEnvelopeMaxEvents {
			return nil, clawXEnvelopeCorrelation{}, errClawXEnvelopeTooLarge
		}
		payloadEventID := clawXEnvelopeID(payloadMap["event_id"])
		if eventID != "" && payloadEventID != "" && eventID != payloadEventID {
			return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("envelope event id mismatch")
		}
		if eventID == "" {
			eventID = payloadEventID
		}
		projectedPayload := clawXProjectEnvelopePayload(itemType, payloadMap, correlationID)
		if traceID == "" {
			traceID = clawXTraceID(projectedPayload)
		}
		encodedItem, err := common.Marshal(map[string]any{"type": itemType})
		if err != nil {
			return nil, clawXEnvelopeCorrelation{}, err
		}
		encodedPayload, err := common.Marshal(projectedPayload)
		if err != nil {
			return nil, clawXEnvelopeCorrelation{}, err
		}
		sanitized.Write(encodedItem)
		sanitized.WriteByte('\n')
		sanitized.Write(encodedPayload)
		sanitized.WriteByte('\n')
		if int64(sanitized.Len()) > clawXEnvelopeMaxSanitizedBytes {
			return nil, clawXEnvelopeCorrelation{}, errClawXEnvelopeTooLarge
		}
		forwardedItems++
	}
	if forwardedItems == 0 || (eventID == "" && traceID == "") {
		return nil, clawXEnvelopeCorrelation{}, fmt.Errorf("envelope has no valid correlatable event")
	}
	return sanitized.Bytes(), clawXEnvelopeCorrelation{RequestID: correlationID, EventID: eventID, TraceID: traceID, EventCount: processedEvents}, nil
}

func clawXSentryEnvelopeURL(rawDSN string) (*url.URL, error) {
	dsn, err := url.Parse(strings.TrimSpace(rawDSN))
	if err != nil || (dsn.Scheme != "http" && dsn.Scheme != "https") || dsn.Host == "" || dsn.User == nil {
		return nil, fmt.Errorf("invalid Sentry DSN")
	}
	if _, hasPassword := dsn.User.Password(); hasPassword || dsn.RawQuery != "" || dsn.Fragment != "" {
		return nil, fmt.Errorf("invalid Sentry DSN")
	}
	projectID := path.Base(strings.TrimSuffix(dsn.Path, "/"))
	if projectID == "." || projectID == ".." || projectID == "/" || projectID == "" {
		return nil, fmt.Errorf("Sentry DSN is missing project id")
	}
	publicKey := dsn.User.Username()
	if publicKey == "" {
		return nil, fmt.Errorf("Sentry DSN is missing public key")
	}
	prefix := strings.TrimSuffix(strings.TrimSuffix(dsn.Path, "/"), "/"+projectID)
	target := &url.URL{
		Scheme: dsn.Scheme,
		Host:   dsn.Host,
		Path:   path.Join(prefix, "api", projectID, "envelope") + "/",
	}
	query := target.Query()
	query.Set("sentry_key", publicKey)
	query.Set("sentry_version", "7")
	query.Set("sentry_client", "uclaw-envelope-tunnel/1")
	target.RawQuery = query.Encode()
	if err := clawXValidateSentryTarget(target); err != nil {
		return nil, err
	}
	return target, nil
}

func clawXEnvelopeTooLarge(c *gin.Context) {
	c.Header("X-UClaw-Error-Code", "envelope_too_large")
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "code": "envelope_too_large"})
}

func clawXValidEnvelopeContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(mediaType, "application/x-sentry-envelope")
}

func clawXSingleHeaderValue(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func ClawXObservabilityEnvelope(c *gin.Context) {
	settings := clawx_client_setting.GetObservability()
	if !settings.Enabled {
		c.Status(http.StatusNotFound)
		return
	}

	now := time.Now()
	allowed, ipProcessLocal, ipRedisFailure := allowClawXObservabilityEvent(
		c.Request.Context(),
		"ip:"+middleware.ClawXClientIP(c),
		clawXEnvelopeIPLimit,
		1,
		clawXEnvelopeIPWindow,
		now,
	)
	if ipRedisFailure {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		c.Status(http.StatusTooManyRequests)
		return
	}
	installValues, hasInstallID := c.Request.URL.Query()["install_id"]
	if !hasInstallID || len(installValues) != 1 {
		c.Status(http.StatusBadRequest)
		return
	}
	installID := strings.ToLower(strings.TrimSpace(installValues[0]))
	if !clawXInstallIDPattern.MatchString(installID) {
		c.Status(http.StatusBadRequest)
		return
	}
	if c.Request.ContentLength > clawXEnvelopeMaxBytes {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	contentType, contentTypeOK := clawXSingleHeaderValue(c.Request.Header, "Content-Type")
	if !contentTypeOK || !clawXValidEnvelopeContentType(contentType) {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	body, err := clawXReadLimited(c.Request.Body, clawXEnvelopeMaxBytes)
	if err != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	contentEncoding := ""
	if encoding, hasEncoding := clawXSingleHeaderValue(c.Request.Header, "Content-Encoding"); hasEncoding {
		contentEncoding = strings.TrimSpace(encoding)
	} else if len(c.Request.Header.Values("Content-Encoding")) > 1 {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	decodedBody, err := clawXDecodeEnvelopeBody(body, contentEncoding)
	if err != nil {
		if strings.Contains(err.Error(), "exceeds limit") {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		if strings.Contains(err.Error(), "unsupported") {
			c.Status(http.StatusUnsupportedMediaType)
			return
		}
		c.Status(http.StatusBadRequest)
		return
	}
	requestID := ""
	if value, single := clawXSingleHeaderValue(c.Request.Header, "X-Request-Id"); single {
		requestID = value
	}
	sanitizedBody, correlation, err := clawXSanitizeEnvelope(decodedBody, installID, requestID)
	if err != nil {
		if errors.Is(err, errClawXEnvelopeTooLarge) {
			clawXEnvelopeTooLarge(c)
			return
		}
		c.Status(http.StatusBadRequest)
		return
	}
	allowed, installProcessLocal, installRedisFailure := allowClawXObservabilityEvent(
		c.Request.Context(),
		"install:"+installID,
		settings.MaxEventsPerHour,
		correlation.EventCount,
		clawXEnvelopeInstallWindow,
		now,
	)
	if installRedisFailure {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		c.Status(http.StatusTooManyRequests)
		return
	}
	if ipProcessLocal || installProcessLocal {
		c.Header("X-UClaw-Observability-Rate-Limit-Scope", "process")
	}
	body, err = clawXEncodeEnvelopeBody(sanitizedBody, contentEncoding)
	if err != nil || int64(len(body)) > clawXEnvelopeMaxBytes {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	target, err := clawXSentryEnvelopeURL(settings.SentryDsn)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	request.Header.Set("Content-Type", "application/x-sentry-envelope")
	request.Header.Set("X-UClaw-Request-Id", correlation.RequestID)
	if correlation.EventID != "" {
		request.Header.Set("X-UClaw-Event-Id", correlation.EventID)
		c.Header("X-UClaw-Event-Id", correlation.EventID)
	}
	if correlation.TraceID != "" {
		request.Header.Set("X-UClaw-Trace-Id", correlation.TraceID)
		c.Header("X-UClaw-Trace-Id", correlation.TraceID)
	}
	c.Header("X-Request-Id", correlation.RequestID)
	if contentEncoding != "" {
		request.Header.Set("Content-Encoding", contentEncoding)
	}
	response, err := clawXSentryHTTPClient.Do(request)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	for _, header := range []string{"Retry-After", "X-Sentry-Rate-Limits"} {
		if value := response.Header.Get(header); value != "" {
			c.Header(header, value)
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
			c.Status(response.StatusCode)
			return
		}
		c.Status(http.StatusBadGateway)
		return
	}
	c.Status(http.StatusAccepted)
}
