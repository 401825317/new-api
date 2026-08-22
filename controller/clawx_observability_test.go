package controller

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/clawx_client_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testClawXInstallID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testClawXEventID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func testClawXEnvelope(payload string) string {
	return `{"event_id":"` + testClawXEventID + `"}` + "\n" + `{"type":"event"}` + "\n" + payload
}

func gzipTestClawXEnvelope(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func configureClawXObservabilityForTest(t *testing.T, settings clawx_client_setting.Observability) {
	t.Helper()
	if settings.TunnelPath == "" {
		settings.TunnelPath = "/api/clawx/observability/envelope"
	}
	if settings.MaxEventsPerHour == 0 {
		settings.MaxEventsPerHour = 30
	}
	if settings.Enabled && settings.SentryDsn == "" {
		settings.SentryDsn = "https://public@example.com/42"
	}
	clientSetting := clawx_client_setting.GetClientSetting()
	previous := clientSetting.Observability
	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	previousRedisEval := clawXRedisRateLimitEval
	previousSentryTargetCheck := clawXIsSafeSentryTarget
	previousSentryClient := clawXSentryHTTPClient
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)
	clientSetting.Observability = string(encoded)
	common.RedisEnabled = false
	common.RDB = nil
	clawXObservabilityLimiter = &clawXSlidingWindowLimiter{events: map[string][]time.Time{}}
	clawXIsSafeSentryTarget = func(target *url.URL) bool {
		return strings.HasPrefix(target.Hostname(), "127.0.0.1") || strings.HasPrefix(target.Hostname(), "[::1]")
	}
	clawXSentryHTTPClient = &http.Client{Timeout: clawXEnvelopeUpstreamTimeout}
	t.Cleanup(func() {
		clientSetting.Observability = previous
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
		clawXRedisRateLimitEval = previousRedisEval
		clawXIsSafeSentryTarget = previousSentryTargetCheck
		clawXSentryHTTPClient = previousSentryClient
		clawXObservabilityLimiter = &clawXSlidingWindowLimiter{events: map[string][]time.Time{}}
	})
}

func sentryDSNForServer(t *testing.T, serverURL string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	require.NoError(t, err)
	parsed.User = url.User("public-key")
	parsed.Path = "/sentry/42"
	return parsed.String()
}

func performClawXEnvelopeRequest(body io.Reader, installID, remoteAddress string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/clawx/observability/envelope?install_id="+url.QueryEscape(installID),
		body,
	)
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", "application/x-sentry-envelope")
	context.Request = request
	ClawXObservabilityEnvelope(context)
	context.Writer.WriteHeaderNow()
	return recorder
}

func TestClawXSentryEnvelopeURL(t *testing.T) {
	t.Setenv("CLAWX_SENTRY_TRUSTED_DOMAINS", "example.com")
	target, err := clawXSentryEnvelopeURL("https://public@example.com/sentry/42")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/sentry/api/42/envelope/?sentry_client=uclaw-envelope-tunnel%2F1&sentry_key=public&sentry_version=7", target.String())

	_, err = clawXSentryEnvelopeURL("https://example.com/42")
	assert.Error(t, err)
	_, err = clawXSentryEnvelopeURL("not-a-dsn")
	assert.Error(t, err)
	_, err = clawXSentryEnvelopeURL("ftp://public@example.com/42")
	assert.Error(t, err)
	_, err = clawXSentryEnvelopeURL("https://public:private-secret@example.com/42")
	assert.Error(t, err)
	_, err = clawXSentryEnvelopeURL("https://public@example.com/42?token=private-secret")
	assert.Error(t, err)
	for _, raw := range []string{
		"https://public@127.0.0.1/42",
		"https://public@10.0.0.1/42",
		"https://public@169.254.169.254/42",
		"https://public@untrusted.example/42",
		"https://public@example.com.attacker.invalid/42",
	} {
		_, err = clawXSentryEnvelopeURL(raw)
		assert.Error(t, err, raw)
	}
}

func TestClawXSentryTransportBlocksProxyAndUnsafeDNSResults(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	transport, ok := clawXSentryTransport().(*http.Transport)
	require.True(t, ok)
	assert.Nil(t, transport.Proxy)
	assert.Equal(t, int64(64*1024), transport.MaxResponseHeaderBytes)

	previousLookup := clawXSentryLookupIP
	t.Cleanup(func() { clawXSentryLookupIP = previousLookup })
	tests := []struct {
		name      string
		addresses []net.IPAddr
	}{
		{name: "loopback", addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}},
		{name: "private", addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}},
		{name: "mixed public and private", addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("169.254.169.254")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clawXSentryLookupIP = func(context.Context, string) ([]net.IPAddr, error) {
				return test.addresses, nil
			}
			connection, err := clawXSentryDialContext(&net.Dialer{Timeout: 50 * time.Millisecond})(context.Background(), "tcp", "sentry.example.com:443")
			assert.Nil(t, connection)
			assert.ErrorContains(t, err, "blocked address")
		})
	}

	lookupCalled := false
	clawXSentryLookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		lookupCalled = true
		return nil, errors.New("unexpected lookup")
	}
	connection, err := clawXSentryDialContext(&net.Dialer{})(context.Background(), "tcp", "127.0.0.1:443")
	assert.Nil(t, connection)
	assert.ErrorContains(t, err, "blocked address")
	assert.False(t, lookupCalled)
	assert.True(t, clawXBlockedSentryIP(net.ParseIP("::ffff:127.0.0.1")))
	assert.True(t, clawXBlockedSentryIP(net.ParseIP("::127.0.0.1")))
	assert.True(t, clawXBlockedSentryIP(net.ParseIP("fec0::1")))
	assert.True(t, clawXBlockedSentryIP(net.ParseIP("3fff::1")))
}

func TestClawXObservabilityEnvelopeForwardsAcceptedEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(testClawXEnvelope(`{"contexts":{"trace":{"trace_id":"cccccccccccccccccccccccccccccccc"}}}`))
	var receivedPath string
	var receivedQuery url.Values
	var receivedContentType string
	var receivedContentEncoding string
	var receivedRequestID string
	var receivedBody []byte
	var receivedBodyError error
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPath = request.URL.Path
		receivedQuery = request.URL.Query()
		receivedContentType = request.Header.Get("Content-Type")
		receivedContentEncoding = request.Header.Get("Content-Encoding")
		receivedRequestID = request.Header.Get("X-UClaw-Request-Id")
		receivedBody, receivedBodyError = io.ReadAll(request.Body)
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:    true,
		SentryDsn:  sentryDSNForServer(t, upstream.URL),
		TunnelPath: "/api/clawx/observability/envelope",
	})
	requestBody := bytes.NewReader(gzipTestClawXEnvelope(t, payload))
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/clawx/observability/envelope?install_id="+strings.ToUpper(testClawXInstallID),
		requestBody,
	)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Content-Type", "application/x-sentry-envelope")
	context.Request = request
	ClawXObservabilityEnvelope(context)
	context.Writer.WriteHeaderNow()

	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "/sentry/api/42/envelope/", receivedPath)
	assert.Equal(t, "public-key", receivedQuery.Get("sentry_key"))
	assert.Equal(t, "7", receivedQuery.Get("sentry_version"))
	assert.Equal(t, "application/x-sentry-envelope", receivedContentType)
	assert.Equal(t, "gzip", receivedContentEncoding)
	require.NoError(t, receivedBodyError)
	decodedBody, err := clawXDecodeEnvelopeBody(receivedBody, receivedContentEncoding)
	require.NoError(t, err)
	assert.Contains(t, string(decodedBody), testClawXEventID)
	assert.Contains(t, string(decodedBody), `"uclaw_request_id"`)
	assert.Equal(t, testClawXEventID, recorder.Header().Get("X-UClaw-Event-Id"))
	assert.Equal(t, "cccccccccccccccccccccccccccccccc", recorder.Header().Get("X-UClaw-Trace-Id"))
	assert.NotEmpty(t, recorder.Header().Get("X-Request-Id"))
	assert.Equal(t, recorder.Header().Get("X-Request-Id"), receivedRequestID)
}

func TestClawXObservabilityEnvelopeRejectsDisabledInvalidAndOversizedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("disabled", func(t *testing.T) {
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: false})
		recorder := performClawXEnvelopeRequest(strings.NewReader("event"), testClawXInstallID, "192.0.2.20:1234")
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("invalid installation id", func(t *testing.T) {
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
		recorder := performClawXEnvelopeRequest(strings.NewReader("event"), "raw-installation-id", "192.0.2.21:1234")
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("oversized body", func(t *testing.T) {
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
		body := bytes.NewReader(make([]byte, clawXEnvelopeMaxBytes+1))
		recorder := performClawXEnvelopeRequest(body, testClawXInstallID, "192.0.2.22:1234")
		assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	})
}

func TestClawXObservabilityEnvelopeRequiresSentryContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})

	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		{name: "missing", wantStatus: http.StatusUnsupportedMediaType},
		{name: "JSON", contentType: "application/json", wantStatus: http.StatusUnsupportedMediaType},
		{name: "ambiguous", contentType: "application/x-sentry-envelope, application/json", wantStatus: http.StatusUnsupportedMediaType},
		{name: "valid with parameter", contentType: "application/x-sentry-envelope; charset=utf-8", wantStatus: http.StatusAccepted},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, "/api/clawx/observability/envelope?install_id="+testClawXInstallID, strings.NewReader(testClawXEnvelope(`{}`)))
			request.RemoteAddr = "192.0.2." + strconv.Itoa(110+index) + ":1234"
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			ginContext.Request = request
			ClawXObservabilityEnvelope(ginContext)
			ginContext.Writer.WriteHeaderNow()
			assert.Equal(t, test.wantStatus, recorder.Code)
		})
	}
	assert.Equal(t, 1, upstreamCalls)
}

func TestClawXObservabilityEnvelopeRejectsDuplicateProtocolHeadersAndQueryIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
	tests := []struct {
		name       string
		mutate     func(*http.Request)
		wantStatus int
	}{
		{name: "content type", wantStatus: http.StatusUnsupportedMediaType, mutate: func(request *http.Request) {
			request.Header.Add("Content-Type", "application/x-sentry-envelope")
		}},
		{name: "content encoding", wantStatus: http.StatusUnsupportedMediaType, mutate: func(request *http.Request) {
			request.Header.Set("Content-Encoding", "gzip")
			request.Header.Add("Content-Encoding", "identity")
		}},
		{name: "install id", wantStatus: http.StatusBadRequest, mutate: func(request *http.Request) {
			request.URL.RawQuery += "&install_id=" + testClawXInstallID
		}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, "/api/clawx/observability/envelope?install_id="+testClawXInstallID, strings.NewReader(testClawXEnvelope(`{}`)))
			request.RemoteAddr = "192.0.2." + strconv.Itoa(130+index) + ":1234"
			request.Header.Set("Content-Type", "application/x-sentry-envelope")
			test.mutate(request)
			ginContext.Request = request
			ClawXObservabilityEnvelope(ginContext)
			ginContext.Writer.WriteHeaderNow()
			assert.Equal(t, test.wantStatus, recorder.Code)
		})
	}
}

func TestClawXObservabilityEnvelopeEnforcesIPAndInstallationLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("ip", func(t *testing.T) {
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
		for index := 0; index < clawXEnvelopeIPLimit; index++ {
			recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), "invalid", "192.0.2.30:1234")
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		}
		recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), "invalid", "192.0.2.30:1234")
		assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	})

	t.Run("installation", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
			Enabled:          true,
			SentryDsn:        sentryDSNForServer(t, upstream.URL),
			MaxEventsPerHour: 3,
		})
		for index := 0; index < 3; index++ {
			remoteAddress := "192.0.2." + strconv.Itoa(40+index) + ":1234"
			recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, remoteAddress)
			assert.Equal(t, http.StatusAccepted, recorder.Code)
		}
		recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.99:1234")
		assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	})
}

func TestClawXObservabilityEnvelopeHidesUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:   true,
		SentryDsn: sentryDSNForServer(t, upstream.URL),
	})

	recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.60:1234")
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
}

func TestClawXObservabilityEnvelopePreservesUpstreamRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "60")
		response.Header().Set("X-Sentry-Rate-Limits", "60:error:organization")
		response.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:          true,
		SentryDsn:        sentryDSNForServer(t, upstream.URL),
		MaxEventsPerHour: 30,
	})

	recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.61:1234")
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Equal(t, "60", recorder.Header().Get("Retry-After"))
	assert.Equal(t, "60:error:organization", recorder.Header().Get("X-Sentry-Rate-Limits"))
}

func TestClawXObservabilityEnvelopePreservesUpstreamClientRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:          true,
		SentryDsn:        sentryDSNForServer(t, upstream.URL),
		MaxEventsPerHour: 30,
	})

	recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.63:1234")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestClawXObservabilityEnvelopeRejectsOversizedContentLengthWithoutReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:          true,
		SentryDsn:        "https://public@example.com/42",
		MaxEventsPerHour: 30,
	})

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/clawx/observability/envelope?install_id="+testClawXInstallID,
		strings.NewReader("small"),
	)
	request.RemoteAddr = "192.0.2.62:1234"
	request.ContentLength = clawXEnvelopeMaxBytes + 1
	request.Header.Set("Content-Type", "application/x-sentry-envelope")
	context.Request = request
	ClawXObservabilityEnvelope(context)
	context.Writer.WriteHeaderNow()

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestClawXObservabilityEnvelopeSanitizesSensitiveContentAndDropsAttachments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "super-secret-value"
	const privatePath = `C:\\Users\\alice\\private.txt`
	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var err error
		received, err = io.ReadAll(request.Body)
		require.NoError(t, err)
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:   true,
		SentryDsn: sentryDSNForServer(t, upstream.URL),
	})
	payload := strings.Join([]string{
		`{"event_id":"` + testClawXEventID + `"}`,
		`{"type":"event"}`,
		`{"message":"` + secret + `","request":{"headers":{"Authorization":"Bearer ` + secret + `"},"data":"` + secret + `"},"exception":{"values":[{"type":"TypeError","value":"` + secret + `","stacktrace":{"frames":[{"filename":"` + privatePath + `","function":"renderArtifact","lineno":42,"vars":{"prompt":"` + secret + `"}}]}}]},"breadcrumbs":[{"message":"` + secret + `","data":{"token":"` + secret + `"}}],"contexts":{"custom":{"prompt":"` + secret + `"}},"extra":{"prompt":"` + secret + `","file_content":"` + secret + `","path":"` + privatePath + `"}}`,
		`{"type":"attachment","length":` + strconv.Itoa(len(secret)) + `}`,
		secret,
	}, "\n")
	recorder := performClawXEnvelopeRequest(strings.NewReader(payload), testClawXInstallID, "192.0.2.70:1234")
	require.Equal(t, http.StatusAccepted, recorder.Code)
	forwarded := string(received)
	assert.NotContains(t, forwarded, secret)
	assert.NotContains(t, forwarded, privatePath)
	assert.NotContains(t, forwarded, `"type":"attachment"`)
	assert.NotContains(t, forwarded, `"message"`)
	assert.NotContains(t, forwarded, `"breadcrumbs"`)
	assert.NotContains(t, forwarded, `"custom"`)
	assert.NotContains(t, forwarded, `"vars"`)
	assert.Contains(t, forwarded, `"type":"TypeError"`)
	assert.Contains(t, forwarded, `"filename":"private.txt"`)
	assert.Contains(t, forwarded, `"function":"renderArtifact"`)
	assert.Contains(t, forwarded, `"lineno":42`)
}

func TestClawXObservabilityEnvelopeUsesProvidedRequestIDAndMarksProcessRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var receivedRequestID string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedRequestID = request.Header.Get("X-UClaw-Request-Id")
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:   true,
		SentryDsn: sentryDSNForServer(t, upstream.URL),
	})
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost,
		"/api/clawx/observability/envelope?install_id="+testClawXInstallID,
		strings.NewReader(testClawXEnvelope(`{}`)),
	)
	request.RemoteAddr = "192.0.2.71:1234"
	request.Header.Set("X-Request-Id", "uclaw-request-0001")
	request.Header.Set("Content-Type", "application/x-sentry-envelope")
	context.Request = request
	ClawXObservabilityEnvelope(context)
	context.Writer.WriteHeaderNow()
	require.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "uclaw-request-0001", recorder.Header().Get("X-Request-Id"))
	assert.Equal(t, "uclaw-request-0001", receivedRequestID)
	assert.Equal(t, "process", recorder.Header().Get("X-UClaw-Observability-Rate-Limit-Scope"))
}

func TestClawXObservabilityRateLimitUsesRemoteAddrUnlessProxyIsTrusted(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.10")
	assert.Equal(t, "192.0.2.10", clawXTrustedProxyClientIP(request))

	t.Setenv("CLAWX_OBSERVABILITY_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	assert.Equal(t, "198.51.100.10", clawXTrustedProxyClientIP(request))

	request.Header.Set("X-Forwarded-For", "198.51.100.10, 192.0.2.11")
	assert.Equal(t, "198.51.100.10", clawXTrustedProxyClientIP(request))
}

func TestClawXObservabilityEnvelopeRejectsEventCountAndSanitizedSizeWith413Code(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	settings := clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)}
	configureClawXObservabilityForTest(t, settings)

	build := func(count int, payload string) string {
		parts := []string{`{"event_id":"` + testClawXEventID + `"}`}
		for index := 0; index < count; index++ {
			parts = append(parts, `{"type":"event"}`, payload)
		}
		return strings.Join(parts, "\n")
	}

	tooMany := performClawXEnvelopeRequest(strings.NewReader(build(clawXEnvelopeMaxEvents+1, `{}`)), testClawXInstallID, "192.0.2.200:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooMany.Code)
	assert.Equal(t, "envelope_too_large", tooMany.Header().Get("X-UClaw-Error-Code"))
	assert.Contains(t, tooMany.Body.String(), `"code":"envelope_too_large"`)

	frame := `{"filename":"` + strings.Repeat("A", 128) + `","function":"` + strings.Repeat("B", 128) + `","lineno":42,"colno":7,"in_app":true}`
	frames := strings.TrimSuffix(strings.Repeat(frame+",", 20), ",")
	exceptionValue := `{"type":"` + strings.Repeat("C", 128) + `","mechanism":{"type":"` + strings.Repeat("D", 128) + `"},"stacktrace":{"frames":[` + frames + `]}}`
	values := strings.TrimSuffix(strings.Repeat(exceptionValue+",", 10), ",")
	largePayload := `{"exception":{"values":[` + values + `]}}`
	tooLarge := performClawXEnvelopeRequest(strings.NewReader(build(4, largePayload)), testClawXInstallID, "192.0.2.201:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.Code)
	assert.Equal(t, "envelope_too_large", tooLarge.Header().Get("X-UClaw-Error-Code"))

	oversizedHeader := `{"event_id":"` + testClawXEventID + `","padding":"` + strings.Repeat("a", clawXEnvelopeMaxHeaderBytes) + `"}` + "\n" + `{"type":"event"}` + "\n{}"
	headerResponse := performClawXEnvelopeRequest(strings.NewReader(oversizedHeader), testClawXInstallID, "192.0.2.202:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, headerResponse.Code)

	oversizedItemHeader := `{"event_id":"` + testClawXEventID + `"}` + "\n" + `{"type":"event","padding":"` + strings.Repeat("a", clawXEnvelopeMaxItemHeaderBytes) + `"}` + "\n{}"
	itemHeaderResponse := performClawXEnvelopeRequest(strings.NewReader(oversizedItemHeader), testClawXInstallID, "192.0.2.203:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, itemHeaderResponse.Code)

	oversizedEvent := testClawXEnvelope(`{"message":"` + strings.Repeat("a", clawXEnvelopeMaxEventBytes) + `"}`)
	eventResponse := performClawXEnvelopeRequest(strings.NewReader(oversizedEvent), testClawXInstallID, "192.0.2.204:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, eventResponse.Code)

	manyItems := []string{`{"event_id":"` + testClawXEventID + `"}`, `{"type":"event"}`, `{}`}
	for index := 0; index < clawXEnvelopeMaxItems; index++ {
		manyItems = append(manyItems, `{"type":"client_report","length":1}`, "x")
	}
	itemCountResponse := performClawXEnvelopeRequest(strings.NewReader(strings.Join(manyItems, "\n")), testClawXInstallID, "192.0.2.205:1234")
	assert.Equal(t, http.StatusRequestEntityTooLarge, itemCountResponse.Code)
	assert.Zero(t, upstreamCalls)
}

func TestClawXObservabilityEnvelopeRejectsUnsupportedEncodingAndExpandedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})

	t.Run("unsupported encoding", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodPost,
			"/api/clawx/observability/envelope?install_id="+testClawXInstallID,
			strings.NewReader(testClawXEnvelope(`{}`)),
		)
		request.RemoteAddr = "192.0.2.72:1234"
		request.Header.Set("Content-Encoding", "br")
		request.Header.Set("Content-Type", "application/x-sentry-envelope")
		context.Request = request
		ClawXObservabilityEnvelope(context)
		context.Writer.WriteHeaderNow()
		assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	})

	t.Run("gzip expansion exceeds decoded limit", func(t *testing.T) {
		payload := []byte(testClawXEnvelope(`{"extra":{"note":"` + strings.Repeat("a", int(clawXEnvelopeMaxDecodedBytes)) + `"}}`))
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodPost,
			"/api/clawx/observability/envelope?install_id="+testClawXInstallID,
			bytes.NewReader(gzipTestClawXEnvelope(t, payload)),
		)
		request.RemoteAddr = "192.0.2.73:1234"
		request.Header.Set("Content-Encoding", "gzip")
		request.Header.Set("Content-Type", "application/x-sentry-envelope")
		context.Request = request
		ClawXObservabilityEnvelope(context)
		context.Writer.WriteHeaderNow()
		assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	})
}

func TestClawXRedisRateLimitKeyDoesNotExposeRawIdentity(t *testing.T) {
	raw := "install:" + testClawXInstallID
	key := clawXRedisRateLimitKey(raw)
	assert.True(t, strings.HasPrefix(key, "rateLimit:CXO:"))
	assert.NotContains(t, key, testClawXInstallID)
	assert.NotContains(t, key, "install:")
}

func TestClawXObservabilityEnvelopeProjectsOnlyControlledDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "prompt-token-file-content-must-not-leave"
	const diagnosticHash = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var err error
		received, err = io.ReadAll(request.Body)
		require.NoError(t, err)
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{
		Enabled:   true,
		SentryDsn: sentryDSNForServer(t, upstream.URL),
	})
	payload := `{"event_id":"` + testClawXEventID + `","level":"error","message":"` + secret + `","exception":{"values":[{"type":"GatewayTimeout","value":"` + secret + `","stacktrace":{"frames":[{"filename":"C:\\Users\\alice\\app.js","function":"startGateway","lineno":17,"colno":9,"in_app":true,"vars":{"token":"` + secret + `"}}]}},{"type":"C:/Users/alice/InternalError","mechanism":{"type":"C:/Users/alice/runtime"},"stacktrace":{"frames":[{"function":"C:/Users/alice/run"}]}}]},"breadcrumbs":[{"message":"` + secret + `","data":{"file_content":"` + secret + `"}}],"contexts":{"trace":{"trace_id":"cccccccccccccccccccccccccccccccc","span_id":"eeeeeeeeeeeeeeee","status":"internal_error"},"custom":{"prompt":"` + secret + `"}},"tags":{"stage":"gateway_start","status":"failed","component":"sk-aaaaaaaaaaaaaaaa","operation":"token","freeform":"` + secret + `"},"extra":{"payload_hash":"` + diagnosticHash + `","input_length":321,"prompt":"` + secret + `","file_content":"` + secret + `"}}`
	recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(payload)), testClawXInstallID, "192.0.2.74:1234")
	require.Equal(t, http.StatusAccepted, recorder.Code)
	forwarded := string(received)
	assert.NotContains(t, forwarded, secret)
	assert.NotContains(t, forwarded, `"message"`)
	assert.NotContains(t, forwarded, `"value"`)
	assert.NotContains(t, forwarded, `"vars"`)
	assert.NotContains(t, forwarded, `"breadcrumbs"`)
	assert.NotContains(t, forwarded, `"custom"`)
	assert.NotContains(t, forwarded, `"freeform"`)
	assert.NotContains(t, forwarded, "C:/Users/alice")
	assert.Contains(t, forwarded, `"type":"GatewayTimeout"`)
	assert.Contains(t, forwarded, `"filename":"app.js"`)
	assert.Contains(t, forwarded, `"function":"startGateway"`)
	assert.Contains(t, forwarded, `"status":"internal_error"`)
	assert.Contains(t, forwarded, `"stage":"gateway_start"`)
	assert.Contains(t, forwarded, `"payload_hash":"`+diagnosticHash+`"`)
	assert.Contains(t, forwarded, `"input_length":321`)
	assert.NotContains(t, forwarded, `"component"`)
	assert.NotContains(t, forwarded, `"operation"`)
}

func TestClawXObservabilityEnvelopeRejectsMalformedJSONAndForwardsEventWithBinaryCrash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for index, itemType := range []string{"event", "transaction"} {
		t.Run("malformed "+itemType+" is rejected", func(t *testing.T) {
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				upstreamCalls++
				response.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
			body := `{"event_id":"` + testClawXEventID + `"}` + "\n" + `{"type":"` + itemType + `"}` + "\n" + `{broken`
			recorder := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2."+strconv.Itoa(75+index)+":1234")
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Zero(t, upstreamCalls)
		})
	}

	t.Run("valid event survives dropped binary crash item", func(t *testing.T) {
		var received []byte
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			var err error
			received, err = io.ReadAll(request.Body)
			require.NoError(t, err)
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
		crash := []byte{0x00, 0xff, 0x7f, 0x01}
		body := append([]byte(testClawXEnvelope(`{"level":"error"}`)+"\n"+`{"type":"minidump","length":4}`+"\n"), crash...)
		recorder := performClawXEnvelopeRequest(bytes.NewReader(body), testClawXInstallID, "192.0.2.76:1234")
		require.Equal(t, http.StatusAccepted, recorder.Code)
		assert.Contains(t, string(received), `"type":"event"`)
		assert.NotContains(t, string(received), "minidump")
		assert.NotContains(t, received, crash)
	})

	t.Run("valid event survives unknown binary item", func(t *testing.T) {
		var received []byte
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			var err error
			received, err = io.ReadAll(request.Body)
			require.NoError(t, err)
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
		binaryPayload := []byte{0x00, 0xff, 0x7f, 0x01}
		body := append([]byte(testClawXEnvelope(`{"level":"error"}`)+"\n"+`{"type":"profile_chunk","length":4}`+"\n"), binaryPayload...)
		recorder := performClawXEnvelopeRequest(bytes.NewReader(body), testClawXInstallID, "192.0.2.79:1234")
		require.Equal(t, http.StatusAccepted, recorder.Code)
		assert.Contains(t, string(received), `"type":"event"`)
		assert.NotContains(t, string(received), "profile_chunk")
		assert.NotContains(t, received, binaryPayload)
	})

	t.Run("binary-only envelope is rejected", func(t *testing.T) {
		upstreamCalls := 0
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			upstreamCalls++
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
		body := `{"event_id":"` + testClawXEventID + `"}` + "\n" + `{"type":"minidump","length":4}` + "\n" + "dump"
		recorder := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2.78:1234")
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Zero(t, upstreamCalls)
	})
}

func TestClawXObservabilityEnvelopeFailsClosedWhenConfiguredRedisErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	clawXRedisRateLimitEval = func(context.Context, string, int, int, time.Duration) (int, error) {
		return 0, errors.New("redis unavailable")
	}
	recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.77:1234")
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-UClaw-Observability-Rate-Limit-Scope"))
}

func TestClawXObservabilityRedisLimitCountsEnvelopeEventsAndRejectsCorruptState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("counts each event", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL), MaxEventsPerHour: 3})
		redisServer := miniredis.RunT(t)
		redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
		t.Cleanup(func() { _ = redisClient.Close() })
		common.RedisEnabled = true
		common.RDB = redisClient

		body := strings.Join([]string{
			`{"event_id":"` + testClawXEventID + `"}`,
			`{"type":"event"}`, `{}`,
			`{"type":"transaction"}`, `{}`,
		}, "\n")
		first := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2.120:1234")
		require.Equal(t, http.StatusAccepted, first.Code)
		second := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2.121:1234")
		assert.Equal(t, http.StatusTooManyRequests, second.Code)
		count, err := redisServer.Get(clawXRedisRateLimitKey("install:" + testClawXInstallID))
		require.NoError(t, err)
		assert.Equal(t, "2", count)
	})

	t.Run("corrupt counter fails closed", func(t *testing.T) {
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
		redisServer := miniredis.RunT(t)
		redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
		t.Cleanup(func() { _ = redisClient.Close() })
		common.RedisEnabled = true
		common.RDB = redisClient
		require.NoError(t, redisServer.Set(clawXRedisRateLimitKey("install:"+testClawXInstallID), "not-a-number"))

		recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.122:1234")
		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	})
}

func TestClawXEnvelopeCorrelationFallbackIsPayloadSpecific(t *testing.T) {
	firstBody := []byte(`{}` + "\n" + `{"type":"event"}` + "\n" + `{"contexts":{"trace":{"trace_id":"11111111111111111111111111111111"}}}`)
	secondBody := []byte(`{}` + "\n" + `{"type":"event"}` + "\n" + `{"contexts":{"trace":{"trace_id":"22222222222222222222222222222222"}}}`)
	_, first, err := clawXSanitizeEnvelope(firstBody, testClawXInstallID, "")
	require.NoError(t, err)
	_, second, err := clawXSanitizeEnvelope(secondBody, testClawXInstallID, "")
	require.NoError(t, err)
	assert.NotEqual(t, first.RequestID, second.RequestID)
	assert.True(t, strings.HasPrefix(first.RequestID, "uclaw-sentry-"))
}

func TestClawXObservabilityEnvelopeRejectsMismatchedEventID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true})
	body := `{"event_id":"` + testClawXEventID + `"}` + "\n" + `{"type":"event"}` + "\n" + `{"event_id":"cccccccccccccccccccccccccccccccc"}`
	recorder := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2.123:1234")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestClawXObservabilityEnvelopeAcceptsOnlyStrictRequestIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		requestID string
		accepted  bool
	}{
		{name: "UUID", requestID: "123e4567-e89b-42d3-a456-426614174000", accepted: true},
		{name: "UClaw", requestID: "uclaw-request-0001", accepted: true},
		{name: "token-like hex", requestID: strings.Repeat("f", 64), accepted: false},
		{name: "arbitrary opaque", requestID: "private-token-looking-value-1234567890", accepted: false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var receivedRequestID string
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				receivedRequestID = request.Header.Get("X-UClaw-Request-Id")
				response.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, "/api/clawx/observability/envelope?install_id="+testClawXInstallID, strings.NewReader(testClawXEnvelope(`{}`)))
			request.RemoteAddr = "192.0.2." + strconv.Itoa(80+index) + ":1234"
			request.Header.Set("X-Request-Id", test.requestID)
			request.Header.Set("Content-Type", "application/x-sentry-envelope")
			ginContext.Request = request
			ClawXObservabilityEnvelope(ginContext)
			ginContext.Writer.WriteHeaderNow()
			require.Equal(t, http.StatusAccepted, recorder.Code)
			if test.accepted {
				assert.Equal(t, test.requestID, recorder.Header().Get("X-Request-Id"))
			} else {
				assert.NotEqual(t, test.requestID, recorder.Header().Get("X-Request-Id"))
				assert.True(t, strings.HasPrefix(recorder.Header().Get("X-Request-Id"), "uclaw-sentry-"))
			}
			assert.Equal(t, recorder.Header().Get("X-Request-Id"), receivedRequestID)
		})
	}
}

func TestClawXObservabilityEnvelopeDistinguishesMissingAndInvalidItemLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		itemHeader string
		payload    string
	}{
		{name: "negative", itemHeader: `{"type":"event","length":-1}`, payload: `{}`},
		{name: "fractional", itemHeader: `{"type":"event","length":1.5}`, payload: `{}`},
		{name: "wrong type", itemHeader: `{"type":"event","length":"2"}`, payload: `{}`},
		{name: "over limit", itemHeader: `{"type":"event","length":5242881}`, payload: `{}`},
		{name: "truncated", itemHeader: `{"type":"event","length":10}`, payload: `{}`},
		{name: "declared boundary mismatch", itemHeader: `{"type":"event","length":2}`, payload: `{}x`},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				upstreamCalls++
				response.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
			body := `{"event_id":"` + testClawXEventID + `"}` + "\n" + test.itemHeader + "\n" + test.payload
			recorder := performClawXEnvelopeRequest(strings.NewReader(body), testClawXInstallID, "192.0.2."+strconv.Itoa(90+index)+":1234")
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Zero(t, upstreamCalls)
		})
	}

	t.Run("missing length uses newline-delimited payload", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		configureClawXObservabilityForTest(t, clawx_client_setting.Observability{Enabled: true, SentryDsn: sentryDSNForServer(t, upstream.URL)})
		recorder := performClawXEnvelopeRequest(strings.NewReader(testClawXEnvelope(`{}`)), testClawXInstallID, "192.0.2.99:1234")
		assert.Equal(t, http.StatusAccepted, recorder.Code)
	})
}
