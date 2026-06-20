package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetClawXRouter(apiRouter *gin.RouterGroup, anonymousRequestBodyLimit gin.HandlerFunc) {
	clawXRoute := apiRouter.Group("/clawx")
	{
		clawXRoute.GET("/bootstrap", controller.ClawXBootstrap)
		clawXRoute.GET("/client-config", controller.ClawXClientConfig)
		clawXRoute.GET("/support-qrcodes/:file", controller.ClawXSupportQRCode)
		clawXRoute.POST("/activation/check", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ClawXActivationCheck)
		clawXRoute.POST("/verification/send-code", middleware.EmailVerificationRateLimit(), anonymousRequestBodyLimit, controller.ClawXSendVerificationCode)
		clawXRoute.POST("/register", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ClawXRegister)
		clawXRoute.POST("/login", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ClawXLogin)
		clawXRoute.POST("/auth/refresh", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ClawXRefresh)
		clawXRoute.POST("/auth/logout", anonymousRequestBodyLimit, controller.ClawXLogout)
		clawXRoute.GET("/updates/latest", controller.ClawXUpdateLatest)
		clawXRoute.GET("/updates/feed/:channel/*file", controller.ClawXUpdateFeed)

		adminRoute := clawXRoute.Group("/admin")
		adminRoute.Use(middleware.RootAuth())
		{
			adminRoute.GET("/releases", controller.AdminListClawXReleases)
			adminRoute.GET("/releases/preview-feed", controller.AdminPreviewClawXReleaseFeed)
			adminRoute.GET("/releases/:id", controller.AdminGetClawXRelease)
			adminRoute.POST("/releases", controller.AdminCreateClawXRelease)
			adminRoute.PUT("/releases/:id", controller.AdminUpdateClawXRelease)
			adminRoute.DELETE("/releases/:id", controller.AdminDeleteClawXRelease)
			adminRoute.POST("/support-qrcode", controller.AdminUploadClawXSupportQRCode)
		}

		authRoute := clawXRoute.Group("/")
		authRoute.Use(middleware.ClawXAuth())
		{
			authRoute.POST("/auth/verify", controller.ClawXAuthVerify)
			authRoute.POST("/auth/unregister-device", controller.ClawXUnregisterDevice)
			authRoute.POST("/relay-token", middleware.CriticalRateLimit(), controller.ClawXRelayToken)
			authRoute.GET("/user/self", controller.ClawXUserSelf)

			billingRoute := authRoute.Group("/billing")
			{
				billingRoute.GET("/checkout-info", controller.ClawXBillingCheckoutInfo)
				billingRoute.GET("/orders/history", controller.ClawXBillingOrderHistory)
				billingRoute.POST("/orders", middleware.CriticalRateLimit(), controller.ClawXBillingCreateOrder)
				billingRoute.POST("/orders/verify", controller.ClawXBillingVerifyOrder)
			}
		}
	}

	compatAuthRoute := apiRouter.Group("/v1/auth")
	{
		compatAuthRoute.POST("/refresh", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ClawXRefresh)
		compatAuthRoute.POST("/logout", anonymousRequestBodyLimit, controller.ClawXLogout)
		compatAuthRoute.GET("/me", middleware.ClawXAuth(), controller.ClawXUserSelf)
	}
}
