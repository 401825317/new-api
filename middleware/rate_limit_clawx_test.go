package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func resetClawXRateLimitTestState(t *testing.T) {
	t.Helper()
	previousRedisEnabled := common.RedisEnabled
	previousGlobalEnabled := common.GlobalApiRateLimitEnable
	previousGlobalNum := common.GlobalApiRateLimitNum
	previousGlobalDuration := common.GlobalApiRateLimitDuration
	previousAPIEnabled := common.ClawXAPIRateLimitEnable
	previousAPINum := common.ClawXAPIRateLimitNum
	previousAPIDuration := common.ClawXAPIRateLimitDuration
	previousAuthEnabled := common.ClawXAuthRateLimitEnable
	previousAuthNum := common.ClawXAuthRateLimitNum
	previousAuthDuration := common.ClawXAuthRateLimitDuration

	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 1
	common.GlobalApiRateLimitDuration = 60
	common.ClawXAPIRateLimitEnable = true
	common.ClawXAPIRateLimitNum = 3
	common.ClawXAPIRateLimitDuration = 60
	common.ClawXAuthRateLimitEnable = true
	common.ClawXAuthRateLimitNum = 1
	common.ClawXAuthRateLimitDuration = 60
	inMemoryRateLimiter = common.InMemoryRateLimiter{}

	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.GlobalApiRateLimitEnable = previousGlobalEnabled
		common.GlobalApiRateLimitNum = previousGlobalNum
		common.GlobalApiRateLimitDuration = previousGlobalDuration
		common.ClawXAPIRateLimitEnable = previousAPIEnabled
		common.ClawXAPIRateLimitNum = previousAPINum
		common.ClawXAPIRateLimitDuration = previousAPIDuration
		common.ClawXAuthRateLimitEnable = previousAuthEnabled
		common.ClawXAuthRateLimitNum = previousAuthNum
		common.ClawXAuthRateLimitDuration = previousAuthDuration
		inMemoryRateLimiter = common.InMemoryRateLimiter{}
	})
}

func performRateLimitRequest(router http.Handler, method string, path string) int {
	return performRateLimitRequestFrom(router, method, path, "198.51.100.10:12345", "")
}

func performRateLimitRequestFrom(router http.Handler, method string, path string, remoteAddr string, forwardedFor string) int {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	router.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestClawXRoutesUseDedicatedAPIRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetClawXRateLimitTestState(t)

	router := gin.New()
	api := router.Group("/api")
	api.Use(GlobalAPIRateLimit())
	clawX := api.Group("/clawx")
	clawX.Use(ClawXTrustedProxy(), ClawXAPIRateLimit())
	clawX.GET("/bootstrap", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	clawX.POST("/auth/verify", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	api.GET("/status", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodGet, "/api/clawx/bootstrap"))
	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodGet, "/api/clawx/bootstrap"))
	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodPost, "/api/clawx/auth/verify"))
	require.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, http.MethodGet, "/api/clawx/bootstrap"))
	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodGet, "/api/status"))
	require.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, http.MethodGet, "/api/status"))
}

func TestClawXTrustedProxyPreventsForwardedIPBucketRotation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetClawXRateLimitTestState(t)
	t.Setenv("CLAWX_OBSERVABILITY_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	common.ClawXAPIRateLimitNum = 1

	router := gin.New()
	router.Use(ClawXTrustedProxy(), ClawXAPIRateLimit())
	router.GET("/limited", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	first := performRateLimitRequestFrom(router, http.MethodGet, "/limited", "192.0.2.10:12345", "203.0.113.10, 198.51.100.20")
	second := performRateLimitRequestFrom(router, http.MethodGet, "/limited", "192.0.2.10:12345", "203.0.113.11, 198.51.100.20")
	require.Equal(t, http.StatusNoContent, first)
	require.Equal(t, http.StatusTooManyRequests, second)
}

func TestClawXTrustedProxyClientIPRejectsAmbiguousHeaders(t *testing.T) {
	t.Setenv("CLAWX_OBSERVABILITY_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:12345"

	request.Header["X-Forwarded-For"] = []string{"198.51.100.10", "198.51.100.11"}
	require.Equal(t, "192.0.2.10", ClawXTrustedProxyClientIP(request))

	request.Header.Del("X-Forwarded-For")
	request.Header.Set("X-Forwarded-For", "invalid, 198.51.100.11")
	require.Equal(t, "192.0.2.10", ClawXTrustedProxyClientIP(request))

	request.Header.Del("X-Forwarded-For")
	request.Header["X-Real-Ip"] = []string{"198.51.100.10", "198.51.100.11"}
	require.Equal(t, "192.0.2.10", ClawXTrustedProxyClientIP(request))

	request.Header.Del("X-Real-Ip")
	request.Header.Set("X-Real-IP", "198.51.100.10")
	require.Equal(t, "198.51.100.10", ClawXTrustedProxyClientIP(request))
}

func TestClawXTrustedProxyRejectsTrustAllCIDRs(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-For", "198.51.100.10")

	t.Setenv("CLAWX_OBSERVABILITY_TRUSTED_PROXY_CIDRS", "0.0.0.0/0,::/0")
	require.Equal(t, "192.0.2.10", ClawXTrustedProxyClientIP(request))
}

func TestClawXAuthRoutesDoNotShareOneCriticalBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetClawXRateLimitTestState(t)

	router := gin.New()
	router.POST("/login", ClawXLoginRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/refresh", ClawXRefreshRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/relay-token", ClawXRelayTokenRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodPost, "/login"))
	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodPost, "/refresh"))
	require.Equal(t, http.StatusNoContent, performRateLimitRequest(router, http.MethodPost, "/relay-token"))
	require.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, http.MethodPost, "/login"))
}
