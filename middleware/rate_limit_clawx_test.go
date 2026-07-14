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
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = "198.51.100.10:12345"
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
	clawX.Use(ClawXAPIRateLimit())
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
