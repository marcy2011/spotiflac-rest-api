package controllers

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(healthCtrl *HealthController, downloadCtrl *DownloadController, apiSecret, allowedOrigin string) *gin.Engine {
	r := gin.Default()
	r.Use(corsMiddleware(allowedOrigin))

	r.GET("/api/health", healthCtrl.HealthCheck)

	api := r.Group("/api")
	api.Use(apiAuthMiddleware(apiSecret))
	api.GET("/status/:id", downloadCtrl.GetStatus)
	api.GET("/status/:id/audio/:index", downloadCtrl.StreamAudio)
	api.POST("/download", downloadCtrl.DownloadAsync)
	api.POST("/download/sync", downloadCtrl.DownloadSync)

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return r
}

func apiAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "API authentication is not configured"})
			return
		}
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, prefix)), []byte(secret)) != 1 {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	}
}

func corsMiddleware(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowedOrigin != "" && origin == allowedOrigin {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
			c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range")
		}
		if c.Request.Method == http.MethodOptions {
			if origin == "" || origin == allowedOrigin {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
