package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	json "github.com/json-iterator/go"
	"github.com/gin-gonic/gin"
)

const statsFilePath = "stats.json"

// saveStats saves the current request statistics to a JSON file.
func saveStats() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	data, err := json.MarshalIndent(requestStats, "", "  ")
	if err != nil {
		log.Printf("Error marshalling stats: %v", err)
		return
	}

	if err := os.WriteFile(statsFilePath, data, 0644); err != nil {
		log.Printf("Error saving stats to %s: %v", statsFilePath, err)
	}
}

// loadStats loads request statistics from a JSON file.
func loadStats() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	data, err := os.ReadFile(statsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("%s not found, starting with fresh statistics.", statsFilePath)
			// Ensure history is not nil
			requestStats.RequestHistory = []RequestRecord{}
		} else {
			log.Printf("Error loading %s: %v", statsFilePath, err)
		}
		return
	}

	if err := json.Unmarshal(data, &requestStats); err != nil {
		log.Printf("Error unmarshalling %s: %v", statsFilePath, err)
	} else {
		log.Printf("Successfully loaded %d request records from %s", len(requestStats.RequestHistory), statsFilePath)
	}

	// Ensure history is not nil even after unmarshalling an old format
	if requestStats.RequestHistory == nil {
		requestStats.RequestHistory = []RequestRecord{}
	}
}


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
				"expiryDate": tokenInfo.ExpiryDate.Format("2006-01-02 15:04:05"),
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
		"currentQPS":   fmt.Sprintf("%.3f", currentQPS),
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

func getTokenDisplayName(account *JetbrainsAccount) string {
	if account.JWT != "" && len(account.JWT) > 10 {
		return "Token ..." + account.JWT[len(account.JWT)-6:]
	}
	if account.LicenseID != "" && len(account.LicenseID) > 10 {
		return "Token ..." + account.LicenseID[len(account.LicenseID)-6:]
	}
	return "Token Unknown"
}

func getLicenseDisplayName(account *JetbrainsAccount) string {
	if account.Authorization != "" && len(account.Authorization) > 20 {
		prefix := account.Authorization[:3]
		suffix := account.Authorization[len(account.Authorization)-3:]
		return prefix + "*" + suffix
	}
	return "Unknown"
}

func getAccountIdentifier(account *JetbrainsAccount) string {
	if account.LicenseID != "" {
		return account.LicenseID
	}
	return "with static JWT"
}


// Statistics functions
func recordRequest(success bool, responseTime int64, model, account string) {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	requestStats.TotalRequests++
	requestStats.LastRequestTime = time.Now()
	requestStats.TotalResponseTime += responseTime

	if success {
		requestStats.SuccessfulRequests++
	} else {
		requestStats.FailedRequests++
	}

	// Add to history (keep last 1000 records)
	record := RequestRecord{
		Timestamp:    time.Now(),
		Success:      success,
		ResponseTime: responseTime,
		Model:        model,
		Account:      account,
	}

	requestStats.RequestHistory = append(requestStats.RequestHistory, record)
	if len(requestStats.RequestHistory) > 1000 {
		requestStats.RequestHistory = requestStats.RequestHistory[1:]
	}
}

func getPeriodStats(hours int) PeriodStats {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var periodRequests int64
	var periodSuccessful int64
	var periodResponseTime int64

	// Calculate requests in the last minute of the period for QPS
	lastMinuteCutoff := time.Now().Add(-1 * time.Minute)
	var lastMinuteRequests int64

	for _, record := range requestStats.RequestHistory {
		if record.Timestamp.After(cutoff) {
			periodRequests++
			periodResponseTime += record.ResponseTime
			if record.Success {
				periodSuccessful++
			}
		}
		if record.Timestamp.After(lastMinuteCutoff) {
			lastMinuteRequests++
		}
	}

	stats := PeriodStats{
		Requests: periodRequests,
	}

	if periodRequests > 0 {
		stats.SuccessRate = float64(periodSuccessful) / float64(periodRequests) * 100
		stats.AvgResponseTime = periodResponseTime / periodRequests
	}

	// Calculate QPS based on the last minute of activity
	stats.QPS = float64(lastMinuteRequests) / 60.0

	return stats
}

func getCurrentQPS() float64 {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	var recentRequests int64

	for _, record := range requestStats.RequestHistory {
		if record.Timestamp.After(cutoff) {
			recentRequests++
		}
	}

	return float64(recentRequests) / 60.0
}

func getTokenInfoFromAccount(account *JetbrainsAccount) (*TokenInfo, error) {
	quotaData, err := getQuotaData(account)
	if err != nil {
		return &TokenInfo{
			Name:   getTokenDisplayName(account),
			Status: "错误",
		}, err
	}

	dailyUsed, _ := strconv.ParseFloat(quotaData.Current.Current.Amount, 64)
	dailyTotal, _ := strconv.ParseFloat(quotaData.Current.Maximum.Amount, 64)

	var usageRate float64
	if dailyTotal > 0 {
		usageRate = (dailyUsed / dailyTotal) * 100
	}

	status := "正常"
	if !account.HasQuota {
		status = "配额不足"
	} else if time.Now().Add(24 * time.Hour).After(account.ExpiryTime) {
		status = "即将过期"
	}

	return &TokenInfo{
		Name:       getTokenDisplayName(account),
		License:    getLicenseDisplayName(account),
		Used:       dailyUsed,
		Total:      dailyTotal,
		UsageRate:  usageRate,
		ExpiryDate: account.ExpiryTime,
		Status:     status,
		HasQuota:   account.HasQuota,
	}, nil
}