package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildClientDiagnosticsCapturesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/chat/completions?ignored=1", strings.NewReader("{}"))
	req.RemoteAddr = "203.0.113.10:51234"
	req.Host = "zz-cn.lingzhiwuxian.com"
	req.Header.Set("User-Agent", "OpenAI/JS 6.8.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.10")
	req.Header.Set("CF-Ray", "abc123-HKG")
	req.Header.Set("X-Oneapi-Request-Id", "req_123")
	req.Header.Set("X-UClaw-Client", "UClaw")
	req.Header.Set("X-UClaw-Version", "0.7.1")
	req.Header.Set("X-UClaw-Platform", "win32")
	req.Header.Set("X-UClaw-Provider", "clawx-openai-image")
	req.Header.Set("X-ClawX-Session-Id", "session_abc")
	c.Request = req

	diagnostics := buildClientDiagnostics(c)
	require.Equal(t, "198.51.100.9", diagnostics["ip"])
	require.Equal(t, http.MethodPost, diagnostics["method"])
	require.Equal(t, "zz-cn.lingzhiwuxian.com", diagnostics["host"])
	require.Equal(t, "/v1/chat/completions", diagnostics["path"])
	require.Equal(t, "HTTP/1.1", diagnostics["proto"])
	require.Equal(t, int64(2), diagnostics["content_length"])
	require.Equal(t, "OpenAI/JS 6.8.0", diagnostics["user_agent"])
	require.Equal(t, "application/json", diagnostics["content_type"])
	require.Equal(t, "text/event-stream", diagnostics["accept"])
	require.Equal(t, "198.51.100.9, 203.0.113.10", diagnostics["x_forwarded_for"])
	require.Equal(t, "abc123-HKG", diagnostics["cf_ray"])
	require.Equal(t, "req_123", diagnostics["x_oneapi_request_id"])
	require.Equal(t, "UClaw", diagnostics["uclaw_client"])
	require.Equal(t, "0.7.1", diagnostics["uclaw_version"])
	require.Equal(t, "win32", diagnostics["uclaw_platform"])
	require.Equal(t, "clawx-openai-image", diagnostics["uclaw_provider"])
	require.Equal(t, "session_abc", diagnostics["clawx_session_id"])
}

func TestBuildClientDiagnosticsTruncatesLongHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	longUserAgent := strings.Repeat("a", maxLoggedHeaderValueLength+32)
	req := httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)
	req.RemoteAddr = "203.0.113.10:51234"
	req.Header.Set("User-Agent", longUserAgent)
	c.Request = req

	diagnostics := buildClientDiagnostics(c)
	require.Equal(t, longUserAgent[:maxLoggedHeaderValueLength]+"...(truncated)", diagnostics["user_agent"])
}
