package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

func ReleaseDrain() gin.HandlerFunc {
	return func(c *gin.Context) {
		done, ok := common.StartReleaseWork()
		if !ok {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "service_unavailable_error", "code": "instance_draining", "message": "Instance is draining"}})
			return
		}
		defer done()
		c.Next()
	}
}
