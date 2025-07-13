package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

const (
	DefaultRequestTimeout = 30 * time.Second
	QuotaCacheTime        = time.Hour
	JWTRefreshTime        = 12 * time.Hour
)

// Global variables
var (
	validClientKeys        = make(map[string]bool)
	jetbrainsAccounts      []JetbrainsAccount
	currentAccountIndex    int
	accountRotationLock    sync.Mutex
	modelsData             ModelsData
	modelsConfig           ModelsConfig // Store the full config for internal model lookup
	anthropicModelMappings map[string]string
	httpClient             *http.Client
	// Statistics tracking
	requestStats RequestStats
	statsMutex   sync.Mutex
)

// Statistics data structures
type RequestStats struct {
	TotalRequests      int64           `json:"total_requests"`
	SuccessfulRequests int64           `json:"successful_requests"`
	FailedRequests     int64           `json:"failed_requests"`
	TotalResponseTime  int64           `json:"total_response_time"` // in milliseconds
	LastRequestTime    time.Time       `json:"last_request_time"`
	RequestHistory     []RequestRecord `json:"request_history"`
}

type RequestRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	Success      bool      `json:"success"`
	ResponseTime int64     `json:"response_time"` // in milliseconds
	Model        string    `json:"model"`
	Account      string    `json:"account"`
}

type PeriodStats struct {
	Requests        int64   `json:"requests"`
	SuccessRate     float64 `json:"success_rate"`
	AvgResponseTime int64   `json:"avg_response_time"`
	QPS             float64 `json:"qps"`
}

type TokenInfo struct {
	Name       string    `json:"name"`
	License    string    `json:"license"`
	Used       float64   `json:"used"`
	Total      float64   `json:"total"`
	UsageRate  float64   `json:"usage_rate"`
	ExpiryDate time.Time `json:"expiry_date"`
	Status     string    `json:"status"`
	HasQuota   bool      `json:"has_quota"`
}

type JWTClaims struct {
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
	Sub string `json:"sub"`
}

// Data structures
type JetbrainsAccount struct {
	LicenseID      string  `json:"licenseId,omitempty"`
	Authorization  string  `json:"authorization,omitempty"`
	JWT            string  `json:"jwt,omitempty"`
	LastUpdated    float64 `json:"last_updated"`
	HasQuota       bool    `json:"has_quota"`
	LastQuotaCheck float64 `json:"last_quota_check"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelsData struct {
	Data []ModelInfo `json:"data"`
}

type ModelList struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelsConfig struct {
	Models                 map[string]string `json:"models"`
	AnthropicModelMappings map[string]string `json:"anthropic_model_mappings"`
}

type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
	Stop        interface{}   `json:"stop,omitempty"`
	ServiceTier string        `json:"service_tier,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatCompletionChoice struct {
	Message      ChatMessage `json:"message"`
	Index        int         `json:"index"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   map[string]int         `json:"usage"`
}

type StreamChoice struct {
	Delta        map[string]interface{} `json:"delta"`
	Index        int                    `json:"index"`
	FinishReason *string                `json:"finish_reason"`
}

type StreamResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// Anthropic compatible structures
type AnthropicContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicMessageRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        interface{}        `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Stream        bool               `json:"stream"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicResponseContent struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
	Text  string                 `json:"text,omitempty"`
}

type AnthropicResponseMessage struct {
	ID           string                     `json:"id"`
	Type         string                     `json:"type"`
	Role         string                     `json:"role"`
	Model        string                     `json:"model"`
	Content      []AnthropicResponseContent `json:"content"`
	StopReason   *string                    `json:"stop_reason"`
	StopSequence *string                    `json:"stop_sequence"`
	Usage        AnthropicUsage             `json:"usage"`
}

type JetbrainsMessage struct {
	Type         string                 `json:"type"`
	Content      string                 `json:"content"`
	FunctionCall *JetbrainsFunctionCall `json:"functionCall,omitempty"`
	FunctionName string                 `json:"functionName,omitempty"`
}

type JetbrainsFunctionCall struct {
	FunctionName string `json:"functionName"`
	Content      string `json:"content"`
}

type JetbrainsPayload struct {
	Prompt     string              `json:"prompt"`
	Profile    string              `json:"profile"`
	Chat       JetbrainsChat       `json:"chat"`
	Parameters JetbrainsParameters `json:"parameters"`
}

type JetbrainsChat struct {
	Messages []JetbrainsMessage `json:"messages"`
}

type JetbrainsParameters struct {
	Data []JetbrainsData `json:"data"`
}

type JetbrainsData struct {
	Type  string `json:"type"`
	FQDN  string `json:"fqdn,omitempty"`
	Value string `json:"value,omitempty"`
}

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
		// Try old format (array of strings)
		var oldConfig struct {
			Models                 []string          `json:"models"`
			AnthropicModelMappings map[string]string `json:"anthropic_model_mappings"`
		}
		if err := json.Unmarshal(data, &oldConfig); err != nil {
			// Try simple array format
			var modelIDs []string
			if err := json.Unmarshal(data, &modelIDs); err != nil {
				log.Printf("Error parsing models.json: %v", err)
				anthropicModelMappings = make(map[string]string)
				return result
			}
			// Convert array to map format (model_id: model_id)
			config.Models = make(map[string]string)
			for _, modelID := range modelIDs {
				config.Models[modelID] = modelID
			}
			config.AnthropicModelMappings = make(map[string]string)
		} else {
			// Convert old format to new format
			config.Models = make(map[string]string)
			for _, modelID := range oldConfig.Models {
				config.Models[modelID] = modelID
			}
			config.AnthropicModelMappings = oldConfig.AnthropicModelMappings
		}
	}

	anthropicModelMappings = config.AnthropicModelMappings
	if anthropicModelMappings == nil {
		anthropicModelMappings = make(map[string]string)
	}

	now := time.Now().Unix()
	for modelKey := range config.Models {
		result.Data = append(result.Data, ModelInfo{
			ID:      modelKey, // Use key as API model ID
			Object:  "model",
			Created: now,
			OwnedBy: "jetbrains-ai",
		})
	}

	log.Printf("Loaded %d model mappings from models.json", len(anthropicModelMappings))
	return result
}

func saveAccountsToFile() {
	// 使用环境变量时，不需要保存到文件
	// 账户状态更新只在内存中保持
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
	jwtsEnv := os.Getenv("JETBRAINS_JWTS")

	var licenseIDs, authorizations, jwts []string

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

	if jwtsEnv != "" {
		jwts = strings.Split(jwtsEnv, ",")
		for i, jwt := range jwts {
			jwts[i] = strings.TrimSpace(jwt)
		}
	}

	// 确保所有数组长度一致
	maxLen := len(licenseIDs)
	if len(authorizations) > maxLen {
		maxLen = len(authorizations)
	}
	if len(jwts) > maxLen {
		maxLen = len(jwts)
	}

	// 扩展数组到相同长度
	for len(licenseIDs) < maxLen {
		licenseIDs = append(licenseIDs, "")
	}
	for len(authorizations) < maxLen {
		authorizations = append(authorizations, "")
	}
	for len(jwts) < maxLen {
		jwts = append(jwts, "")
	}

	jetbrainsAccounts = []JetbrainsAccount{}
	for i := 0; i < maxLen; i++ {
		if licenseIDs[i] != "" || authorizations[i] != "" || jwts[i] != "" {
			account := JetbrainsAccount{
				LicenseID:      licenseIDs[i],
				Authorization:  authorizations[i],
				JWT:            jwts[i],
				LastUpdated:    0,
				HasQuota:       true,
				LastQuotaCheck: 0,
			}
			if account.LicenseID == "" {
				account.LicenseID = ""
			}
			if account.Authorization == "" {
				account.Authorization = ""
			}
			if account.JWT == "" {
				account.JWT = ""
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

func authenticateClient(c *gin.Context) {
	if len(validClientKeys) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service unavailable: no client API keys configured"})
		c.Abort()
		return
	}

	authHeader := c.GetHeader("Authorization")
	apiKey := c.GetHeader("x-api-key")

	// Check x-api-key first
	if apiKey != "" {
		if validClientKeys[apiKey] {
			return
		}
		c.JSON(http.StatusForbidden, gin.H{"error": "Invalid client API key (x-api-key)"})
		c.Abort()
		return
	}

	// Check Authorization header
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if validClientKeys[token] {
			return
		}
		c.JSON(http.StatusForbidden, gin.H{"error": "Invalid client API key (Bearer token)"})
		c.Abort()
		return
	}

	c.JSON(http.StatusUnauthorized, gin.H{"error": "API key required in Authorization header (Bearer) or x-api-key header"})
	c.Abort()
}

func checkQuota(account *JetbrainsAccount) error {
	if account.JWT == "" && account.LicenseID != "" {
		if err := refreshJetbrainsJWT(account); err != nil {
			return err
		}
	}

	if account.JWT == "" {
		account.HasQuota = false
		return nil
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Content-Length", "0")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("grazie-agent", `{"name":"aia:pycharm","version":"251.26094.80.13:251.26094.141"}`)
	req.Header.Set("grazie-authenticate-jwt", account.JWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		account.HasQuota = false
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 && account.LicenseID != "" {
		log.Printf("JWT for %s expired, refreshing...", account.LicenseID)
		if err := refreshJetbrainsJWT(account); err != nil {
			return err
		}

		req.Header.Set("grazie-authenticate-jwt", account.JWT)
		resp, err = httpClient.Do(req)
		if err != nil {
			account.HasQuota = false
			return err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		account.HasQuota = false
		return fmt.Errorf("quota check failed with status %d", resp.StatusCode)
	}

	var quotaData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		account.HasQuota = false
		return err
	}

	dailyUsed, _ := quotaData["dailyUsed"].(float64)
	dailyTotal, _ := quotaData["dailyTotal"].(float64)
	if dailyTotal == 0 {
		dailyTotal = 1
	}

	account.HasQuota = dailyUsed < dailyTotal
	if !account.HasQuota {
		log.Printf("Account %s has no quota", getAccountIdentifier(account))
	}

	account.LastQuotaCheck = float64(time.Now().Unix())
	go saveAccountsToFile()

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

	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("authorization", "Bearer "+account.Authorization)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("JWT refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	state, _ := data["state"].(string)
	token, _ := data["token"].(string)

	if state == "PAID" && token != "" {
		account.JWT = token
		account.LastUpdated = float64(time.Now().Unix())
		log.Printf("Successfully refreshed JWT for licenseId %s", account.LicenseID)
		go saveAccountsToFile()
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

func extractTextContent(content interface{}) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var textParts []string
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
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

func getQuotaData(account *JetbrainsAccount) (gin.H, error) {
	if account.JWT == "" && account.LicenseID != "" {
		if err := refreshJetbrainsJWT(account); err != nil {
			return nil, fmt.Errorf("failed to refresh JWT: %w", err)
		}
	}

	if account.JWT == "" {
		return nil, fmt.Errorf("account has no JWT")
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Content-Length", "0")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("grazie-agent", `{"name":"aia:pycharm","version":"251.26094.80.13:251.26094.141"}`)
	req.Header.Set("grazie-authenticate-jwt", account.JWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 && account.LicenseID != "" {
		log.Printf("JWT for %s expired, refreshing...", getAccountIdentifier(account))
		if err := refreshJetbrainsJWT(account); err != nil {
			return nil, err
		}

		req.Header.Set("grazie-authenticate-jwt", account.JWT)
		resp, err = httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
	}

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

	// Parse the JetBrains API response structure
	var dailyUsed, dailyTotal float64

	// Extract from the nested structure
	if current, ok := quotaData["current"].(map[string]interface{}); ok {
		// Get used amount
		if currentUsage, ok := current["current"].(map[string]interface{}); ok {
			if amountStr, ok := currentUsage["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyUsed = parsed
				}
			}
		}

		// Get maximum amount
		if maximum, ok := current["maximum"].(map[string]interface{}); ok {
			if amountStr, ok := maximum["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyTotal = parsed
				}
			}
		}
	}

	// Ensure the values are set in the map for the template
	quotaData["dailyUsed"] = dailyUsed
	quotaData["dailyTotal"] = dailyTotal

	if dailyTotal == 0 {
		dailyTotal = 1 // Avoid division by zero
	}
	account.HasQuota = dailyUsed < dailyTotal
	account.LastQuotaCheck = float64(time.Now().Unix())
	quotaData["HasQuota"] = account.HasQuota // Add to map for template

	return quotaData, nil
}

func showStatsPage(c *gin.Context) {
	// 获取Token信息
	var tokensInfo []TokenInfo
	for i := range jetbrainsAccounts {
		tokenInfo, err := getTokenInfoFromAccount(&jetbrainsAccounts[i])
		if err != nil {
			log.Printf("Error getting token info for account %d: %v", i, err)
			tokenInfo = &TokenInfo{
				Name:   getTokenDisplayName(&jetbrainsAccounts[i]),
				Status: "错误",
			}
		}
		tokensInfo = append(tokensInfo, *tokenInfo)
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
		expiryTime := getTokenExpiryTime(account.JWT)

		status := "正常"
		warning := "正常"
		if time.Now().Add(24 * time.Hour).After(expiryTime) {
			status = "即将过期"
			warning = "即将过期"
		}

		expiryInfo = append(expiryInfo, gin.H{
			"Name":       getTokenDisplayName(account),
			"ExpiryTime": expiryTime.Format("2006-01-02 15:04:05"),
			"Status":     status,
			"Warning":    warning,
		})
	}

	c.HTML(http.StatusOK, "stats.html", gin.H{
		"CurrentTime":  time.Now().Format("2006-01-02 15:04:05"),
		"CurrentQPS":   fmt.Sprintf("%.2f", currentQPS),
		"TotalRecords": requestStats.TotalRequests,
		"Stats24h":     stats24h,
		"Stats7d":      stats7d,
		"Stats30d":     stats30d,
		"TokensInfo":   tokensInfo,
		"ExpiryInfo":   expiryInfo,
		"Timestamp":    time.Now().Format(time.RFC1123),
	})
}

func streamLog(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Create a pipe to capture log output
	r, w, _ := os.Pipe()
	originalStderr := os.Stderr
	os.Stderr = w
	log.SetOutput(w)

	defer func() {
		os.Stderr = originalStderr
		log.SetOutput(originalStderr)
	}()

	scanner := bufio.NewScanner(r)
	go func() {
		for scanner.Scan() {
			fmt.Fprintf(c.Writer, "data: %s\n\n", scanner.Text())
			c.Writer.Flush()
		}
	}()

	// Keep the connection open
	<-c.Request.Context().Done()
}

// API Handlers
func listModels(c *gin.Context) {
	modelList := ModelList{
		Object: "list",
		Data:   modelsData.Data,
	}
	c.JSON(http.StatusOK, modelList)
}

func chatCompletions(c *gin.Context) {
	startTime := time.Now()
	var request ChatCompletionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), "", "")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	modelConfig := getModelItem(request.Model)
	if modelConfig == nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, "")
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model %s not found", request.Model)})
		return
	}

	account, err := getNextJetbrainsAccount()
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, "")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}

	accountIdentifier := getAccountIdentifier(account)

	// Convert OpenAI format to JetBrains format
	toolIDToFuncNameMap := make(map[string]string)
	for _, msg := range request.Messages {
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolIDToFuncNameMap[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	var jetbrainsMessages []JetbrainsMessage
	for _, msg := range request.Messages {
		textContent := extractTextContent(msg.Content)

		switch msg.Role {
		case "user", "system":
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    msg.Role + "_message",
				Content: textContent,
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				firstToolCall := msg.ToolCalls[0]
				toolIDToFuncNameMap[firstToolCall.ID] = firstToolCall.Function.Name
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:    "assistant_message",
					Content: textContent,
					FunctionCall: &JetbrainsFunctionCall{
						FunctionName: firstToolCall.Function.Name,
						Content:      firstToolCall.Function.Arguments,
					},
				})
			} else {
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:    "assistant_message",
					Content: textContent,
				})
			}
		case "tool":
			functionName := toolIDToFuncNameMap[msg.ToolCallID]
			if functionName != "" {
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:         "function_message",
					Content:      textContent,
					FunctionName: functionName,
				})
			} else {
				log.Printf("Warning: Cannot find function name for tool_call_id %s", msg.ToolCallID)
			}
		default:
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    "user_message",
				Content: textContent,
			})
		}
	}

	var data []JetbrainsData
	var tools []ToolFunction
	if request.Tools != nil {
		data = append(data, JetbrainsData{Type: "json", FQDN: "llm.parameters.functions"})
		for _, tool := range request.Tools {
			tools = append(tools, tool.Function)
		}
		toolsJSON, _ := json.Marshal(tools)
		data = append(data, JetbrainsData{Type: "json", Value: string(toolsJSON)})
	}
	// Ensure data is never nil - initialize as empty slice
	if data == nil {
		data = []JetbrainsData{}
	}

	// Use internal model name for JetBrains API call
	internalModel := getInternalModelName(request.Model)
	payload := JetbrainsPayload{
		Prompt:     "ij.chat.request.new-chat-on-start",
		Profile:    internalModel, // Use internal model name
		Chat:       JetbrainsChat{Messages: jetbrainsMessages},
		Parameters: JetbrainsParameters{Data: data},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request"})
		return
	}

	if gin.Mode() == gin.DebugMode {
		log.Printf("Sending payload to JetBrains API: %s", string(payloadBytes))
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/llm/chat/stream/v7", bytes.NewBuffer(payloadBytes))
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("grazie-agent", `{"name":"aia:pycharm","version":"251.26094.80.13:251.26094.141"}`)
	req.Header.Set("grazie-authenticate-jwt", account.JWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to make request"})
		return
	}
	defer resp.Body.Close()

	// Output JetBrains API response status and headers
	if gin.Mode() == gin.DebugMode {
		log.Printf("JetBrains API Response Status: %d", resp.StatusCode)
		log.Printf("JetBrains API Response Headers: %+v", resp.Header)
	}

	if resp.StatusCode == 477 {
		log.Printf("Account %s has no quota (received 477)", getAccountIdentifier(account))
		account.HasQuota = false
		account.LastQuotaCheck = float64(time.Now().Unix())
		go saveAccountsToFile()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("API Error: Status %d, Body: %s", resp.StatusCode, string(body))
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	if request.Stream {
		// Handle streaming response
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		streamID := "chatcmpl-" + uuid.New().String()
		firstChunkSent := false
		var currentTool *map[string]interface{}

		// Variables for assembling complete response content
		var assembledContent []string
		var assembledFunctionCalls []map[string]string

		scanner := bufio.NewScanner(resp.Body)
		success := true
		for scanner.Scan() {
			line := scanner.Text()

			// Log each line received from JetBrains API
			if gin.Mode() == gin.DebugMode {
				log.Printf("JetBrains API Response Line: %s", line)
			}

			if !strings.HasPrefix(line, "data: ") || line == "data: end" {
				continue
			}

			dataStr := line[6:]
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				continue
			}

			eventType, _ := data["type"].(string)

			if eventType == "Content" {
				content, _ := data["content"].(string)
				if content == "" {
					continue
				}

				// Collect content for assembly
				assembledContent = append(assembledContent, content)

				var deltaPayload map[string]interface{}
				if !firstChunkSent {
					deltaPayload = map[string]interface{}{
						"role":    "assistant",
						"content": content,
					}
					firstChunkSent = true
				} else {
					deltaPayload = map[string]interface{}{
						"content": content,
					}
				}

				streamResp := StreamResponse{
					ID:      streamID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   request.Model,
					Choices: []StreamChoice{{Delta: deltaPayload}},
				}

				respJSON, _ := json.Marshal(streamResp)
				c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(respJSON))))
				c.Writer.Flush()
			} else if eventType == "FunctionCall" {
				funcNameInterface := data["name"]
				funcArgs, _ := data["content"].(string)

				var funcName string
				if funcNameInterface == nil {
					funcName = "" // This will be converted to "None" in output
				} else {
					funcName, _ = funcNameInterface.(string)
				}

				// Debug logging
				if gin.Mode() == gin.DebugMode {
					log.Printf("DEBUG: funcNameInterface = %v, funcName = '%s', funcArgs = '%s'", funcNameInterface, funcName, funcArgs)
				}

				// Collect function calls for assembly - match Python behavior
				assembledFunctionCalls = append(assembledFunctionCalls, map[string]string{
					"name":      funcName,
					"arguments": funcArgs,
				})

				if funcName != "" {
					// Start of new function call
					currentTool = &map[string]interface{}{
						"index": 0,
						"id":    fmt.Sprintf("call_%s", uuid.New().String()),
						"function": map[string]interface{}{
							"arguments": "",
							"name":      funcName,
						},
						"type": "function",
					}
				} else if currentTool != nil {
					// Continuation of function arguments
					if funcMap, ok := (*currentTool)["function"].(map[string]interface{}); ok {
						currentArgs, _ := funcMap["arguments"].(string)
						funcMap["arguments"] = currentArgs + funcArgs
					}
				}
			} else if eventType == "FinishMetadata" {
				// Output assembled complete content
				if gin.Mode() == gin.DebugMode {
					if len(assembledContent) > 0 {
						fullContent := strings.Join(assembledContent, "")
						log.Printf("Assembled Content: %s", fullContent)
					}
				}

				// Output assembled function calls
				if gin.Mode() == gin.DebugMode {
					for _, fc := range assembledFunctionCalls {
						name := fc["name"]
						if name == "" {
							name = "None"
						}
						args := fc["arguments"]
						if args == "" {
							args = ""
						}
						log.Printf("Assembled Function Call: %s with args: %s", name, args)
					}
				}

				// Send the complete tool call if we have one
				if currentTool != nil {
					deltaPayload := map[string]interface{}{
						"tool_calls": []map[string]interface{}{*currentTool},
					}
					streamResp := StreamResponse{
						ID:      streamID,
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   request.Model,
						Choices: []StreamChoice{{Delta: deltaPayload}},
					}
					respJSON, _ := json.Marshal(streamResp)
					c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(respJSON))))
					c.Writer.Flush()
				}

				finalResp := StreamResponse{
					ID:      streamID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   request.Model,
					Choices: []StreamChoice{{Delta: map[string]interface{}{}, FinishReason: stringPtr("tool_calls")}},
				}

				respJSON, _ := json.Marshal(finalResp)
				c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(respJSON))))
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				c.Writer.Flush()
				break
			}
		}

		recordRequest(success, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
	} else {
		// Handle non-streaming response - aggregate the stream
		var contentParts []string
		var toolCalls []ToolCall
		var currentFuncName string
		var currentFuncArgs string

		// Variables for assembling complete response content
		var assembledContent []string
		var assembledFunctionCalls []map[string]string

		scanner := bufio.NewScanner(resp.Body)
		success := true
		for scanner.Scan() {
			line := scanner.Text()

			// Log each line received from JetBrains API
			if gin.Mode() == gin.DebugMode {
				log.Printf("JetBrains API Response Line: %s", line)
			}

			if !strings.HasPrefix(line, "data: ") || line == "data: end" {
				continue
			}

			dataStr := line[6:]
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				continue
			}

			eventType, _ := data["type"].(string)

			if eventType == "Content" {
				if content, ok := data["content"].(string); ok {
					contentParts = append(contentParts, content)
					// Collect content for assembly
					assembledContent = append(assembledContent, content)
				}
			} else if eventType == "FunctionCall" {
				if gin.Mode() == gin.DebugMode {
					log.Printf("Received FunctionCall event: %v", data)
				}
				funcNameInterface := data["name"]
				funcArgs, _ := data["content"].(string)

				var funcName string
				if funcNameInterface == nil {
					funcName = "" // This will be converted to "None" in output
				} else {
					funcName, _ = funcNameInterface.(string)
				}

				// Debug logging
				if gin.Mode() == gin.DebugMode {
					log.Printf("DEBUG: funcNameInterface = %v, funcName = '%s', funcArgs = '%s'", funcNameInterface, funcName, funcArgs)
				}
				if gin.Mode() == gin.DebugMode {
					log.Printf("Function name: %s, Arguments: %s", funcName, funcArgs)
				}

				// Collect function calls for assembly - match Python behavior
				assembledFunctionCalls = append(assembledFunctionCalls, map[string]string{
					"name":      funcName,
					"arguments": funcArgs,
				})

				if funcName != "" {
					// Start of new function call
					currentFuncName = funcName
					currentFuncArgs = ""
				}
				// Always append content (arguments come in chunks)
				currentFuncArgs += funcArgs
			} else if eventType == "FinishMetadata" {
				// Output assembled complete content
				if gin.Mode() == gin.DebugMode {
					if len(assembledContent) > 0 {
						fullContent := strings.Join(assembledContent, "")
						log.Printf("Assembled Content: %s", fullContent)
					}
				}

				// Output assembled function calls
				if gin.Mode() == gin.DebugMode {
					for _, fc := range assembledFunctionCalls {
						name := fc["name"]
						if name == "" {
							name = "None"
						}
						args := fc["arguments"]
						if args == "" {
							args = ""
						}
						log.Printf("Assembled Function Call: %s with args: %s", name, args)
					}
				}

				// Complete the function call if we have one
				if currentFuncName != "" {
					toolCalls = append(toolCalls, ToolCall{
						ID:   fmt.Sprintf("call_%s", uuid.New().String()),
						Type: "function",
						Function: Function{
							Name:      currentFuncName,
							Arguments: currentFuncArgs,
						},
					})
				}
				break
			}
		}

		fullContent := strings.Join(contentParts, "")
		message := ChatMessage{
			Role:    "assistant",
			Content: fullContent,
		}

		finishReason := "stop"
		if len(toolCalls) > 0 {
			message.ToolCalls = toolCalls
			finishReason = "tool_calls"
		}

		response := ChatCompletionResponse{
			ID:      "chatcmpl-" + uuid.New().String(),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   request.Model,
			Choices: []ChatCompletionChoice{{
				Message:      message,
				Index:        0,
				FinishReason: finishReason,
			}},
			Usage: map[string]int{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		}

		recordRequest(success, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(http.StatusOK, response)
	}
}

func stringPtr(s string) *string {
	return &s
}

// JWT parsing functions
func parseJWT(tokenString string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode payload (second part)
	payload := parts[1]
	// Add padding if necessary
	if len(payload)%4 != 0 {
		payload += strings.Repeat("=", 4-len(payload)%4)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %v", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %v", err)
	}

	return &claims, nil
}

func getTokenExpiryTime(jwt string) time.Time {
	if jwt == "" {
		return time.Time{}
	}

	claims, err := parseJWT(jwt)
	if err != nil {
		log.Printf("Error parsing JWT: %v", err)
		return time.Time{}
	}

	return time.Unix(claims.Exp, 0)
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

	expiryTime := getTokenExpiryTime(account.JWT)

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

func main() {
	// 加载.env文件
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize HTTP client
	httpClient = &http.Client{Timeout: DefaultRequestTimeout}

	// Load configuration
	modelsData = loadModels()
	// Load the full config for internal model lookup
	data, err := os.ReadFile("models.json")
	if err == nil {
		json.Unmarshal(data, &modelsConfig)
	}
	loadClientAPIKeys()
	loadJetbrainsAccounts()

	// Setup Gin router
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		ginMode = gin.ReleaseMode
	}
	gin.SetMode(ginMode)
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Add custom template functions
	funcMap := template.FuncMap{
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
	}
	r.SetFuncMap(funcMap)

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "*")
		c.Header("Access-Control-Allow-Headers", "*")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	r.LoadHTMLGlob("templates/*")
	r.GET("/", showStatsPage)
	r.GET("/log", streamLog)

	// API routes
	api := r.Group("/v1")
	api.Use(authenticateClient)
	{
		api.GET("/models", listModels)
		api.POST("/chat/completions", chatCompletions)
	}

	log.Println("Starting JetBrains AI OpenAI Compatible API server...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "7860"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
