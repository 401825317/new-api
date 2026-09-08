package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime/debug"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var _bp = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		h := sha256.Sum256([]byte(bi.Main.Path))
		return hex.EncodeToString(h[:4])
	}
	return common.GetRandomString(8)
}()

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := common.GetTimeString() + _bp + common.GetRandomString(8)
		// Keep the gateway's own request ID authoritative, but retain the
		// caller's correlation ID so chained gateways can be searched together.
		parentID := firstCorrelationHeader(c)
		if parentID != "" {
			c.Set(common.ParentRequestIdKey, parentID)
		}
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		c.Next()
	}
}

func firstCorrelationHeader(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	for _, name := range []string{common.ParentRequestIdKey, "X-Request-ID", "X-Client-Request-ID", "X-Oneapi-Request-Id"} {
		value := strings.TrimSpace(c.GetHeader(name))
		if isSafeCorrelationID(value) {
			return value
		}
	}
	return ""
}

// isSafeCorrelationID keeps untrusted request headers out of logs and avoids
// forwarding control characters to the next HTTP hop. IDs are intentionally
// limited to the common trace/UUID alphabet; malformed values are ignored.
func isSafeCorrelationID(value string) bool {
	if value == "" || len(value) > 200 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' && r != '.' && r != ':' {
			return false
		}
	}
	return true
}
