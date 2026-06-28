package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTryGrokVideoAccelRedirectDisabled(t *testing.T) {
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT", "")
	c, recorder := newVideoProxyTestContext()
	c.Request.Header.Set("X-Video-Proxy-Accel", "1")

	if tryGrokVideoAccelRedirect(c, "https://assets.x.ai/video.mp4?token=abc") {
		t.Fatal("tryGrokVideoAccelRedirect should be disabled by default")
	}
	if recorder.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("unexpected X-Accel-Redirect: %q", recorder.Header().Get("X-Accel-Redirect"))
	}
}

func TestTryGrokVideoAccelRedirectRequiresInternalHeader(t *testing.T) {
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT", "true")
	c, recorder := newVideoProxyTestContext()

	if tryGrokVideoAccelRedirect(c, "https://assets.x.ai/video.mp4?token=abc") {
		t.Fatal("tryGrokVideoAccelRedirect should require the internal header")
	}
	if recorder.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("unexpected X-Accel-Redirect: %q", recorder.Header().Get("X-Accel-Redirect"))
	}
}

func TestTryGrokVideoAccelRedirectSupportsSharedToken(t *testing.T) {
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT", "true")
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT_TOKEN", "shared-token")
	c, recorder := newVideoProxyTestContext()
	c.Request.Header.Set("X-Video-Proxy-Accel", "shared-token")

	if !tryGrokVideoAccelRedirect(c, "https://assets.x.ai/video.mp4?token=abc") {
		t.Fatal("tryGrokVideoAccelRedirect should accept the shared token")
	}
	if got, want := recorder.Header().Get("X-Accel-Redirect"), "/__grok_xai/assets.x.ai/video.mp4?token=abc"; got != want {
		t.Fatalf("X-Accel-Redirect = %q, want %q", got, want)
	}
}

func TestTryGrokVideoAccelRedirectRejectsWrongSharedToken(t *testing.T) {
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT", "true")
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT_TOKEN", "shared-token")
	c, recorder := newVideoProxyTestContext()
	c.Request.Header.Set("X-Video-Proxy-Accel", "1")

	if tryGrokVideoAccelRedirect(c, "https://assets.x.ai/video.mp4?token=abc") {
		t.Fatal("tryGrokVideoAccelRedirect should reject the wrong shared token")
	}
	if recorder.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("unexpected X-Accel-Redirect: %q", recorder.Header().Get("X-Accel-Redirect"))
	}
}

func TestTryGrokVideoAccelRedirect(t *testing.T) {
	t.Setenv("GROK_VIDEO_ACCEL_REDIRECT", "true")
	c, recorder := newVideoProxyTestContext()
	c.Request.Header.Set("X-Video-Proxy-Accel", "1")

	if !tryGrokVideoAccelRedirect(c, "https://assets.x.ai/path/video.mp4?token=abc") {
		t.Fatal("tryGrokVideoAccelRedirect should handle valid URLs")
	}
	if got, want := recorder.Header().Get("X-Accel-Redirect"), "/__grok_xai/assets.x.ai/path/video.mp4?token=abc"; got != want {
		t.Fatalf("X-Accel-Redirect = %q, want %q", got, want)
	}
	if got, want := recorder.Header().Get("X-Accel-Buffering"), "no"; got != want {
		t.Fatalf("X-Accel-Buffering = %q, want %q", got, want)
	}
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func newVideoProxyTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/video/grok/task_test", nil)
	return c, recorder
}
