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
	req.Header.Set("Authorization", "Bearer must-not-be-logged")
	req.Header.Set("Cookie", "session=must-not-be-logged")
	req.Header.Set("User-Agent", "OpenAI/JS 6.8.0")
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.10")
	req.Header.Set("X-Oneapi-Request-Id", "req_123")
	req.Header.Set("X-UClaw-Client", "desktop")
	req.Header.Set("X-UClaw-Version", "0.7.1")
	req.Header.Set("X-UClaw-Commit", "0123456789abcdef")
	req.Header.Set("X-UClaw-Build-Id", "build-123")
	req.Header.Set("X-UClaw-Platform", "win32")
	req.Header.Set("X-UClaw-Arch", "x64")
	req.Header.Set("X-UClaw-Channel", "stable")
	req.Header.Set("X-UClaw-Mode", "portable")
	req.Header.Set("X-Request-Id", "uclaw-request-123")
	req.Header.Set("X-UClaw-Provider", "clawx-openai-image")
	req.Header.Set("X-ClawX-Session-Id", "session_abc")
	c.Request = req
	c.Set("clawx_device_id", "device-test")
	c.Set("clawx_session_id", "session-test")

	diagnostics := buildClientDiagnostics(c)
	require.Equal(t, map[string]interface{}{
		"uclaw_client":     "desktop",
		"uclaw_version":    "0.7.1",
		"uclaw_commit":     "0123456789abcdef",
		"uclaw_build_id":   "build-123",
		"uclaw_platform":   "win32",
		"uclaw_arch":       "x64",
		"uclaw_channel":    "stable",
		"uclaw_mode":       "portable",
		"uclaw_request_id": "uclaw-request-123",
	}, diagnostics)
}

func TestBuildClientDiagnosticsRequiresExplicitUClawClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)
	req.Header.Set("X-UClaw-Version", "2.0.3")
	c.Request = req
	c.Set("clawx_device_id", "device-test")
	c.Set("clawx_session_id", "session-test")

	require.Nil(t, buildClientDiagnostics(c))
}

func TestBuildClientDiagnosticsDropsUnsafeIdentifierValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)
	req.Header.Set("X-UClaw-Client", "desktop")
	req.Header.Set("X-UClaw-Version", "2.0.3")
	req.Header.Set("X-UClaw-Commit", `C:\Users\Alice\private.txt`)
	req.Header.Set("X-UClaw-Build-Id", "Bearer secret-token")
	req.Header.Set("X-UClaw-Platform", strings.Repeat("a", maxUClawRuntimeLabelLength+1))
	req.Header.Set("X-UClaw-Arch", "x64")
	req.Header.Set("X-Request-Id", "request id containing a prompt")
	c.Request = req
	c.Set("clawx_device_id", "device-test")
	c.Set("clawx_session_id", "session-test")

	require.Equal(t, map[string]interface{}{
		"uclaw_client":  "desktop",
		"uclaw_version": "2.0.3",
		"uclaw_arch":    "x64",
	}, buildClientDiagnostics(c))
}

func TestReplaceClientDiagnosticsRemovesCallerSuppliedSensitiveData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)
	req.Header.Set("X-UClaw-Client", "desktop")
	req.Header.Set("X-UClaw-Version", "2.0.3")
	req.Header.Set("X-UClaw-Platform", "win32")
	c.Request = req
	c.Set("clawx_device_id", "device-test")
	c.Set("clawx_session_id", "session-test")

	other := map[string]interface{}{
		"cache_tokens": 12,
		"client_diagnostics": map[string]interface{}{
			"authorization": "Bearer private-token",
			"prompt":        "private prompt",
			"path":          `C:\Users\Alice\private.txt`,
		},
	}
	replaceClientDiagnostics(c, other)

	require.Equal(t, 12, other["cache_tokens"])
	require.Equal(t, map[string]interface{}{
		"uclaw_client":   "desktop",
		"uclaw_version":  "2.0.3",
		"uclaw_platform": "win32",
	}, other["client_diagnostics"])
}

func TestReplaceClientDiagnosticsRemovesMetadataFromNonUClawRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)

	other := map[string]interface{}{
		"client_diagnostics": map[string]interface{}{
			"authorization": "Bearer private-token",
		},
	}
	replaceClientDiagnostics(c, other)

	require.NotContains(t, other, "client_diagnostics")
}
