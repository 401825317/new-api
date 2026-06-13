package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ClawXAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		user, session, err := model.ValidateClawXAccessToken(c.Request.Header.Get("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "clawx auth token invalid or expired",
				"code":    "auth_invalid",
			})
			c.Abort()
			return
		}
		c.Set("id", user.Id)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("status", user.Status)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		c.Set("clawx_device_id", session.DeviceId)
		c.Set("clawx_session_id", session.Id)
		c.Next()
	}
}
