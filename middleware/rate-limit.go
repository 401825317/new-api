package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var redisRateLimitScript = redis.NewScript(`
local max_requests = tonumber(ARGV[1])
local window_start = ARGV[2]
local now = ARGV[3]
local ttl_seconds = tonumber(ARGV[4])

if max_requests <= 0 then
    return 0
end

local length = redis.call('LLEN', KEYS[1])
if length < max_requests then
    redis.call('LPUSH', KEYS[1], now)
    redis.call('EXPIRE', KEYS[1], ttl_seconds)
    return 1
end

local oldest = redis.call('LINDEX', KEYS[1], -1)
if not string.match(oldest, '^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d%.%d%d%dZ$') then
    return -1
end
if oldest >= window_start then
    redis.call('EXPIRE', KEYS[1], ttl_seconds)
    return 0
end

redis.call('LPUSH', KEYS[1], now)
redis.call('LTRIM', KEYS[1], 0, max_requests - 1)
redis.call('EXPIRE', KEYS[1], ttl_seconds)
return 1
`)

var inMemoryRateLimiter common.InMemoryRateLimiter

const clawXClientIPContextKey = "clawx_trusted_client_ip"

func clawXConfiguredNetworks(env string) []*net.IPNet {
	var networks []*net.IPNet
	for _, raw := range strings.Split(os.Getenv(env), ",") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip = ip.To4()
				bits = 32
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			prefixBits, addressBits := network.Mask.Size()
			if prefixBits == 0 || addressBits == 0 {
				continue
			}
			networks = append(networks, network)
		}
	}
	return networks
}

func clawXRemoteIP(remoteAddr string) net.IP {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr)); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.TrimSpace(remoteAddr))
}

func clawXIPInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func clawXForwardedIPs(value string) ([]net.IP, bool) {
	if len(value) > 4096 {
		return nil, false
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 || len(parts) > 32 {
		return nil, false
	}
	addresses := make([]net.IP, 0, len(parts))
	for _, raw := range parts {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			return nil, false
		}
		addresses = append(addresses, ip)
	}
	return addresses, true
}

func ClawXTrustedProxyClientIP(request *http.Request) string {
	remoteIP := clawXRemoteIP(request.RemoteAddr)
	if remoteIP == nil {
		return "0.0.0.0"
	}
	trusted := clawXConfiguredNetworks("CLAWX_OBSERVABILITY_TRUSTED_PROXY_CIDRS")
	if !clawXIPInNetworks(remoteIP, trusted) {
		return remoteIP.String()
	}

	forwardedValues := request.Header.Values("X-Forwarded-For")
	if len(forwardedValues) > 1 {
		return remoteIP.String()
	}
	forwarded := ""
	if len(forwardedValues) == 1 {
		forwarded = strings.TrimSpace(forwardedValues[0])
	}
	var addresses []net.IP
	if forwarded != "" {
		var valid bool
		addresses, valid = clawXForwardedIPs(forwarded)
		if !valid {
			return remoteIP.String()
		}
	} else if realIPValues := request.Header.Values("X-Real-IP"); len(realIPValues) > 1 {
		return remoteIP.String()
	} else if realIP := strings.TrimSpace(request.Header.Get("X-Real-IP")); realIP != "" {
		if len(realIP) > 64 {
			return remoteIP.String()
		}
		ip := net.ParseIP(realIP)
		if ip == nil {
			return remoteIP.String()
		}
		addresses = []net.IP{ip}
	}

	for index := len(addresses) - 1; index >= 0; index-- {
		if !clawXIPInNetworks(addresses[index], trusted) {
			return addresses[index].String()
		}
	}
	if len(addresses) > 0 {
		return addresses[0].String()
	}
	return remoteIP.String()
}

// ClawXClientIP ignores forwarded headers unless the direct peer is explicitly trusted.
func ClawXClientIP(c *gin.Context) string {
	if value, exists := c.Get(clawXClientIPContextKey); exists {
		if clientIP, ok := value.(string); ok && clientIP != "" {
			return clientIP
		}
	}
	return ClawXTrustedProxyClientIP(c.Request)
}

// ClawXTrustedProxy normalizes forwarded headers before any downstream ClawX middleware.
func ClawXTrustedProxy() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := ClawXTrustedProxyClientIP(c.Request)
		c.Set(clawXClientIPContextKey, clientIP)
		c.Request.Header.Del("X-Forwarded-For")
		c.Request.Header.Del("X-Real-IP")
		c.Request.Header.Set("X-Forwarded-For", clientIP)
		c.Next()
	}
}

var defNext = func(c *gin.Context) {
	c.Next()
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string, clientIP string) {
	ctx := c.Request.Context()
	key := "rateLimit:" + mark + clientIP
	allowed, err := redisSlidingWindowAllowed(ctx, common.RDB, key, maxRequestNum, duration)
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
	}
}

// redisSlidingWindowAllowed updates the existing list-based limiter atomically.
// Timestamps keep the existing sortable string format for compatibility with older keys.
func redisSlidingWindowAllowed(ctx context.Context, rdb *redis.Client, key string, maxRequestNum int, duration int64) (bool, error) {
	if rdb == nil {
		return false, fmt.Errorf("Redis rate limiter is unavailable")
	}
	nowTime := time.Now()
	now := nowTime.Format(timeFormat)
	windowStart := nowTime.Add(-time.Duration(duration) * time.Second).Format(timeFormat)
	ttlSeconds := int64(common.RateLimitKeyExpirationDuration / time.Second)
	result, err := redisRateLimitScript.Run(ctx, rdb, []string{key}, maxRequestNum, windowStart, now, ttlSeconds).Int()
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, fmt.Errorf("invalid rate limit timestamp")
	}
	return result == 1, nil
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string, clientIP string) {
	key := mark + clientIP
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return
	}
}

func rateLimitFactoryWithClientIP(maxRequestNum int, duration int64, mark string, clientIP func(*gin.Context) string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark, clientIP(c))
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark, clientIP(c))
		}
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	return rateLimitFactoryWithClientIP(maxRequestNum, duration, mark, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		limiter := rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
		return func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api/clawx/") || strings.HasPrefix(path, "/api/v1/auth/") {
				c.Next()
				return
			}
			limiter(c)
		}
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func ClawXAPIRateLimit() func(c *gin.Context) {
	if common.ClawXAPIRateLimitEnable {
		return rateLimitFactoryWithClientIP(common.ClawXAPIRateLimitNum, common.ClawXAPIRateLimitDuration, "CXA", ClawXClientIP)
	}
	return defNext
}

func clawXAuthRateLimit(mark string) func(c *gin.Context) {
	if common.ClawXAuthRateLimitEnable {
		return rateLimitFactoryWithClientIP(common.ClawXAuthRateLimitNum, common.ClawXAuthRateLimitDuration, mark, ClawXClientIP)
	}
	return defNext
}

func ClawXLoginRateLimit() func(c *gin.Context) {
	return clawXAuthRateLimit("CXL")
}

func ClawXRefreshRateLimit() func(c *gin.Context) {
	return clawXAuthRateLimit("CXR")
}

func ClawXRelayTokenRateLimit() func(c *gin.Context) {
	return clawXAuthRateLimit("CXT")
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := fmt.Sprintf("rateLimit:%s:user:%d", mark, userId)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := c.Request.Context()
	allowed, err := redisSlidingWindowAllowed(ctx, common.RDB, key, maxRequestNum, duration)
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
