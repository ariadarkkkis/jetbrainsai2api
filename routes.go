package main

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// setupRoutes 设置所有路由
func setupRoutes() *gin.Engine {
	// Setup Gin router
	ginMode := getGinMode()
	gin.SetMode(ginMode)
	r := gin.New()
	
	// 添加中间件
	setupMiddleware(r)
	
	// 设置静态页面路由（不需要认证）
	setupPublicRoutes(r)
	
	// 设置API路由（需要认证）
	setupAPIRoutes(r)
	
	return r
}

// getGinMode 获取Gin运行模式
func getGinMode() string {
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		return gin.ReleaseMode
	}
	return ginMode
}

// setupMiddleware 设置中间件
func setupMiddleware(r *gin.Engine) {
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	
	// 添加CORS中间件
	r.Use(corsMiddleware())
}

// corsMiddleware CORS中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "*")
		c.Header("Access-Control-Allow-Headers", "*")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// setupPublicRoutes 设置公共路由（无需认证）
func setupPublicRoutes(r *gin.Engine) {
	r.GET("/", showStatsPage)
	r.GET("/log", streamLog)
	r.GET("/api/stats", getStatsData)
	r.GET("/health", healthCheck)
}

// setupAPIRoutes 设置API路由（需要认证）
func setupAPIRoutes(r *gin.Engine) {
	api := r.Group("/v1")
	api.Use(authenticateClient)
	{
		api.GET("/models", listModels)
		api.POST("/chat/completions", chatCompletions)
		// Add Anthropic compatible endpoint
		api.POST("/messages", anthropicMessages)
	}
}

// healthCheck 健康检查端点
func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": "healthy",
		"service": "jetbrainsai2api",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"accounts": len(jetbrainsAccounts),
		"valid_keys": len(validClientKeys),
	})
}