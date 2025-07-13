package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Helper functions
func loadModels() ModelsData {
	var result ModelsData

	data, err := os.ReadFile("models.json")
	if err != nil {
		log.Printf("Error loading models.json: %v", err)
		anthropicModelMappings = make(map[string]string)
		return result
	}

	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		// 尝试旧格式（字符串数组）
		var modelIDs []string
		if err := json.Unmarshal(data, &modelIDs); err != nil {
			log.Printf("Error parsing models.json: %v", err)
			anthropicModelMappings = make(map[string]string)
			return result
		}
		// 转换为新格式
		config.Models = make(map[string]string)
		for _, modelID := range modelIDs {
			config.Models[modelID] = modelID
		}
		config.AnthropicModelMappings = make(map[string]string)
	}

	anthropicModelMappings = config.AnthropicModelMappings
	if anthropicModelMappings == nil {
		anthropicModelMappings = make(map[string]string)
	}

	now := time.Now().Unix()
	for modelKey := range config.Models {
		result.Data = append(result.Data, ModelInfo{
			ID:      modelKey,
			Object:  "model",
			Created: now,
			OwnedBy: "jetbrains-ai",
		})
	}

	log.Printf("Loaded %d model mappings from models.json", len(anthropicModelMappings))
	return result
}

func loadClientAPIKeys() {
	// 从环境变量获取API密钥，支持逗号分隔的多个密钥
	keysEnv := os.Getenv("CLIENT_API_KEYS")
	if keysEnv != "" {
		keys := strings.Split(keysEnv, ",")
		validClientKeys = make(map[string]bool)
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" {
				validClientKeys[key] = true
			}
		}

		if len(validClientKeys) == 0 {
			log.Println("Warning: CLIENT_API_KEYS environment variable is empty")
		} else {
			log.Printf("Successfully loaded %d client API keys from environment", len(validClientKeys))
		}
	} else {
		log.Println("Error: CLIENT_API_KEYS environment variable not found")
		validClientKeys = make(map[string]bool)
	}
}

func loadJetbrainsAccounts() {
	// 从环境变量加载账户信息
	licenseIDsEnv := os.Getenv("JETBRAINS_LICENSE_IDS")
	authorizationsEnv := os.Getenv("JETBRAINS_AUTHORIZATIONS")

	var licenseIDs, authorizations []string

	if licenseIDsEnv != "" {
		licenseIDs = strings.Split(licenseIDsEnv, ",")
		for i, id := range licenseIDs {
			licenseIDs[i] = strings.TrimSpace(id)
		}
	}

	if authorizationsEnv != "" {
		authorizations = strings.Split(authorizationsEnv, ",")
		for i, auth := range authorizations {
			authorizations[i] = strings.TrimSpace(auth)
		}
	}

	// 确保所有数组长度一致
	maxLen := len(licenseIDs)
	if len(authorizations) > maxLen {
		maxLen = len(authorizations)
	}

	// 扩展数组到相同长度
	for len(licenseIDs) < maxLen {
		licenseIDs = append(licenseIDs, "")
	}
	for len(authorizations) < maxLen {
		authorizations = append(authorizations, "")
	}

	jetbrainsAccounts = []JetbrainsAccount{}
	for i := 0; i < maxLen; i++ {
		if licenseIDs[i] != "" && authorizations[i] != "" {
			account := JetbrainsAccount{
				LicenseID:      licenseIDs[i],
				Authorization:  authorizations[i],
				JWT:            "",
				LastUpdated:    0,
				HasQuota:       true,
				LastQuotaCheck: 0,
			}
			jetbrainsAccounts = append(jetbrainsAccounts, account)
		}
	}

	if len(jetbrainsAccounts) == 0 {
		log.Println("Warning: No valid JetBrains accounts found in environment variables")
	} else {
		log.Printf("Successfully loaded %d JetBrains AI accounts from environment", len(jetbrainsAccounts))
	}
}

func getInternalModelName(modelID string) string {
	// Check if it's in the models config
	if internalModel, exists := modelsConfig.Models[modelID]; exists {
		return internalModel
	}
	// Fallback to the original model ID
	return modelID
}

func getModelItem(modelID string) *ModelInfo {
	for _, model := range modelsData.Data {
		if model.ID == modelID {
			return &model
		}
	}
	return nil
}

// 统一的HTTP请求头设置
func setJetbrainsHeaders(req *http.Request, jwt string) {
	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("grazie-agent", `{"name":"aia:pycharm","version":"251.26094.80.13:251.26094.141"}`)
	if jwt != "" {
		req.Header.Set("grazie-authenticate-jwt", jwt)
	}
}

// 处理JWT过期并重试请求的通用方法
func handleJWTExpiredAndRetry(req *http.Request, account *JetbrainsAccount) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// 如果JWT过期且有LicenseID，尝试刷新JWT并重试
	if resp.StatusCode == 401 && account.LicenseID != "" {
		resp.Body.Close()
		log.Printf("JWT for %s expired, refreshing...", getAccountIdentifier(account))
		if err := refreshJetbrainsJWT(account); err != nil {
			return nil, err
		}

		// 更新请求头中的JWT并重试
		req.Header.Set("grazie-authenticate-jwt", account.JWT)
		return httpClient.Do(req)
	}

	return resp, nil
}

// 确保账户有有效的JWT
func ensureValidJWT(account *JetbrainsAccount) error {
	if account.JWT == "" && account.LicenseID != "" {
		return refreshJetbrainsJWT(account)
	}
	return nil
}

// processQuotaData 处理配额数据，返回使用量信息并更新账户状态
func processQuotaData(quotaData map[string]any, account *JetbrainsAccount) (dailyUsed, dailyTotal float64) {
	dailyUsed, dailyTotal = extractQuotaUsage(quotaData)
	
	if dailyTotal == 0 {
		dailyTotal = 1 // Avoid division by zero
	}
	
	account.HasQuota = dailyUsed < dailyTotal
	if !account.HasQuota {
		log.Printf("Account %s has no quota", getAccountIdentifier(account))
	}
	
	// 从配额数据中提取过期时间
	if expiryTime := parseExpiryTime(quotaData); !expiryTime.IsZero() {
		account.ExpiryTime = expiryTime
	}
	
	account.LastQuotaCheck = float64(time.Now().Unix())
	return dailyUsed, dailyTotal
}
func checkQuota(account *JetbrainsAccount) error {
	if err := ensureValidJWT(account); err != nil {
		return err
	}

	if account.JWT == "" {
		account.HasQuota = false
		return nil
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Length", "0")
	setJetbrainsHeaders(req, account.JWT)

	resp, err := handleJWTExpiredAndRetry(req, account)
	if err != nil {
		account.HasQuota = false
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		account.HasQuota = false
		return fmt.Errorf("quota check failed with status %d", resp.StatusCode)
	}

	var quotaData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		account.HasQuota = false
		return err
	}

	// 使用统一的配额处理函数
	processQuotaData(quotaData, account)

	return nil
}

func refreshJetbrainsJWT(account *JetbrainsAccount) error {
	log.Printf("Refreshing JWT for licenseId %s...", account.LicenseID)

	payload := map[string]string{"licenseId": account.LicenseID}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/auth/jetbrains-jwt/provide-access/license/v2", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", "Bearer "+account.Authorization)
	setJetbrainsHeaders(req, "")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("JWT refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	state, _ := data["state"].(string)
	token, _ := data["token"].(string)

	if state == "PAID" && token != "" {
		account.JWT = token
		account.LastUpdated = float64(time.Now().Unix())
		log.Printf("Successfully refreshed JWT for licenseId %s", account.LicenseID)
		return nil
	}

	return fmt.Errorf("JWT refresh failed: invalid response state %s", state)
}

func getNextJetbrainsAccount() (*JetbrainsAccount, error) {
	if len(jetbrainsAccounts) == 0 {
		return nil, fmt.Errorf("service unavailable: no JetBrains accounts configured")
	}

	accountRotationLock.Lock()
	defer accountRotationLock.Unlock()

	for i := 0; i < len(jetbrainsAccounts); i++ {
		account := &jetbrainsAccounts[currentAccountIndex]
		currentAccountIndex = (currentAccountIndex + 1) % len(jetbrainsAccounts)

		now := time.Now().Unix()
		isQuotaStale := float64(now)-account.LastQuotaCheck > QuotaCacheTime.Seconds()

		if account.HasQuota && isQuotaStale {
			checkQuota(account)
		}

		if account.HasQuota {
			if account.LicenseID != "" {
				isJWTStale := float64(now)-account.LastUpdated > JWTRefreshTime.Seconds()
				if account.JWT == "" || isJWTStale {
					if err := refreshJetbrainsJWT(account); err != nil {
						log.Printf("Failed to refresh JWT: %v", err)
						continue
					}
					if !account.HasQuota {
						checkQuota(account)
						if !account.HasQuota {
							continue
						}
					}
				}
			}

			if account.JWT != "" {
				return account, nil
			}
		}
	}

	return nil, fmt.Errorf("all JetBrains accounts are over quota or invalid")
}

func getAccountIdentifier(account *JetbrainsAccount) string {
	if account.LicenseID != "" {
		return account.LicenseID
	}
	return "with static JWT"
}

func extractTextContent(content any) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []any:
		var textParts []string
		for _, item := range v {
			if itemMap, ok := item.(map[string]any); ok {
				if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
					if text, ok := itemMap["text"].(string); ok {
						textParts = append(textParts, text)
					}
				}
			}
		}
		return strings.Join(textParts, " ")
	}
	return ""
}

// 解析配额响应中的过期时间
func parseExpiryTime(quotaData map[string]any) time.Time {
	if untilStr, ok := quotaData["until"].(string); ok {
		parsedTime, err := time.Parse(time.RFC3339Nano, untilStr)
		if err != nil {
			parsedTime, err = time.Parse(time.RFC3339, untilStr)
		}
		if err == nil {
			return parsedTime
		}
		log.Printf("Warning: could not parse 'until' date '%s': %v", untilStr, err)
	}
	return time.Time{}
}

// 从配额数据中提取使用量信息
func extractQuotaUsage(quotaData map[string]any) (float64, float64) {
	var dailyUsed, dailyTotal float64

	// Extract from the nested structure
	if current, ok := quotaData["current"].(map[string]any); ok {
		// Get used amount
		if currentUsage, ok := current["current"].(map[string]any); ok {
			if amountStr, ok := currentUsage["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyUsed = parsed
				}
			}
		}

		// Get maximum amount
		if maximum, ok := current["maximum"].(map[string]any); ok {
			if amountStr, ok := maximum["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyTotal = parsed
				}
			}
		}
	}
	return dailyUsed, dailyTotal
}

func getQuotaData(account *JetbrainsAccount) (gin.H, error) {
	if err := ensureValidJWT(account); err != nil {
		return nil, fmt.Errorf("failed to refresh JWT: %w", err)
	}

	if account.JWT == "" {
		return nil, fmt.Errorf("account has no JWT")
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Length", "0")
	setJetbrainsHeaders(req, account.JWT)

	resp, err := handleJWTExpiredAndRetry(req, account)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quota check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var quotaData gin.H
	if err := json.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		return nil, err
	}

	// Debug: 打印API响应数据
	if gin.Mode() == gin.DebugMode {
		quotaJSON, _ := json.MarshalIndent(quotaData, "", "  ")
		log.Printf("JetBrains Quota API Response: %s", string(quotaJSON))
	}

	// 使用统一的配额处理函数
	dailyUsed, dailyTotal := processQuotaData(quotaData, account)

	// Ensure the values are set in the map for the template
	quotaData["dailyUsed"] = dailyUsed
	quotaData["dailyTotal"] = dailyTotal
	quotaData["HasQuota"] = account.HasQuota
	quotaData["expiryTime"] = account.ExpiryTime

	return quotaData, nil
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

	for _, record := range requestStats.RequestHistory {
		if record.Timestamp.After(cutoff) {
			periodRequests++
			periodResponseTime += record.ResponseTime
			if record.Success {
				periodSuccessful++
			}
		}
	}

	stats := PeriodStats{
		Requests: periodRequests,
	}

	if periodRequests > 0 {
		stats.SuccessRate = float64(periodSuccessful) / float64(periodRequests) * 100
		stats.AvgResponseTime = periodResponseTime / periodRequests
		stats.QPS = float64(periodRequests) / float64(hours) / 3600.0
	}

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

	dailyUsed, _ := quotaData["dailyUsed"].(float64)
	dailyTotal, _ := quotaData["dailyTotal"].(float64)

	var usageRate float64
	if dailyTotal > 0 {
		usageRate = (dailyUsed / dailyTotal) * 100
	}

	var expiryTime time.Time
	if expiryTimeInt, ok := quotaData["expiryTime"]; ok {
		if expiry, ok := expiryTimeInt.(time.Time); ok {
			expiryTime = expiry
		}
	}

	status := "正常"
	if !account.HasQuota {
		status = "配额不足"
	} else if time.Now().Add(24 * time.Hour).After(expiryTime) {
		status = "即将过期"
	}

	return &TokenInfo{
		Name:       getTokenDisplayName(account),
		License:    getLicenseDisplayName(account),
		Used:       dailyUsed,
		Total:      dailyTotal,
		UsageRate:  usageRate,
		ExpiryDate: expiryTime,
		Status:     status,
		HasQuota:   account.HasQuota,
	}, nil
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