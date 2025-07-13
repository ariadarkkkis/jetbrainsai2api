package main

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// showStatsPage 显示统计页面
func showStatsPage(c *gin.Context) {
	// 提供静态HTML文件
	c.File("./static/index.html")
}

// getStatsData 获取统计数据的JSON API端点
func getStatsData(c *gin.Context) {
	// 获取Token信息
	var tokensInfo []gin.H
	for i := range jetbrainsAccounts {
		tokenInfo, err := getTokenInfoFromAccount(&jetbrainsAccounts[i])
		if err != nil {
			tokensInfo = append(tokensInfo, gin.H{
				"name":       getTokenDisplayName(&jetbrainsAccounts[i]),
				"license":    "",
				"used":       0.0,
				"total":      0.0,
				"usageRate":  0.0,
				"expiryDate": "",
				"status":     "错误",
			})
		} else {
			tokensInfo = append(tokensInfo, gin.H{
				"name":       tokenInfo.Name,
				"license":    tokenInfo.License,
				"used":       tokenInfo.Used,
				"total":      tokenInfo.Total,
				"usageRate":  tokenInfo.UsageRate,
				"expiryDate": tokenInfo.ExpiryDate.Format("2006/1/2 14:30:20"),
				"status":     tokenInfo.Status,
			})
		}
	}

	// 获取统计数据
	stats24h := getPeriodStats(24)
	stats7d := getPeriodStats(24 * 7)
	stats30d := getPeriodStats(24 * 30)
	currentQPS := getCurrentQPS()

	// 准备Token过期监控数据
	var expiryInfo []gin.H
	for i := range jetbrainsAccounts {
		account := &jetbrainsAccounts[i]
		expiryTime := account.ExpiryTime

		status := "正常"
		warning := "正常"
		if time.Now().Add(24 * time.Hour).After(expiryTime) {
			status = "即将过期"
			warning = "即将过期"
		}

		expiryInfo = append(expiryInfo, gin.H{
			"name":       getTokenDisplayName(account),
			"expiryTime": expiryTime.Format("2006-01-02 15:04:05"),
			"status":     status,
			"warning":    warning,
		})
	}

	// 返回JSON数据
	c.JSON(200, gin.H{
		"currentTime":  time.Now().Format("2006-01-02 15:04:05"),
		"currentQPS":   fmt.Sprintf("%.2f", currentQPS),
		"totalRecords": requestStats.TotalRequests,
		"stats24h":     stats24h,
		"stats7d":      stats7d,
		"stats30d":     stats30d,
		"tokensInfo":   tokensInfo,
		"expiryInfo":   expiryInfo,
	})
}

// streamLog 流式日志输出
func streamLog(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Keep the connection open
	<-c.Request.Context().Done()
}