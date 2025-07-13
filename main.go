package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
	anthropicModelMappings map[string]string
	httpClient             *http.Client
)

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
	Models                 []string          `json:"models"`
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
		var modelIDs []string
		if err := json.Unmarshal(data, &modelIDs); err != nil {
			log.Printf("Error parsing models.json: %v", err)
			anthropicModelMappings = make(map[string]string)
			return result
		}
		config.Models = modelIDs
		config.AnthropicModelMappings = make(map[string]string)
	}

	anthropicModelMappings = config.AnthropicModelMappings
	if anthropicModelMappings == nil {
		anthropicModelMappings = make(map[string]string)
	}

	now := time.Now().Unix()
	for _, modelID := range config.Models {
		result.Data = append(result.Data, ModelInfo{
			ID:      modelID,
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

// API Handlers
func listModels(c *gin.Context) {
	modelList := ModelList{
		Object: "list",
		Data:   modelsData.Data,
	}
	c.JSON(http.StatusOK, modelList)
}

func chatCompletions(c *gin.Context) {
	var request ChatCompletionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	modelConfig := getModelItem(request.Model)
	if modelConfig == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model %s not found", request.Model)})
		return
	}

	account, err := getNextJetbrainsAccount()
	if err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}

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

	payload := JetbrainsPayload{
		Prompt:     "ij.chat.request.new-chat-on-start",
		Profile:    request.Model,
		Chat:       JetbrainsChat{Messages: jetbrainsMessages},
		Parameters: JetbrainsParameters{Data: data},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request"})
		return
	}

	log.Printf("Sending payload to JetBrains API: %s", string(payloadBytes))

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/llm/chat/stream/v7", bytes.NewBuffer(payloadBytes))
	if err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to make request"})
		return
	}
	defer resp.Body.Close()

	// Output JetBrains API response status and headers
	log.Printf("JetBrains API Response Status: %d", resp.StatusCode)
	log.Printf("JetBrains API Response Headers: %+v", resp.Header)

	if resp.StatusCode == 477 {
		log.Printf("Account %s has no quota (received 477)", getAccountIdentifier(account))
		account.HasQuota = false
		account.LastQuotaCheck = float64(time.Now().Unix())
		go saveAccountsToFile()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("API Error: Status %d, Body: %s", resp.StatusCode, string(body))
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
		for scanner.Scan() {
			line := scanner.Text()

			// Log each line received from JetBrains API
			log.Printf("JetBrains API Response Line: %s", line)

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
				log.Printf("DEBUG: funcNameInterface = %v, funcName = '%s', funcArgs = '%s'", funcNameInterface, funcName, funcArgs)

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
				if len(assembledContent) > 0 {
					fullContent := strings.Join(assembledContent, "")
					log.Printf("Assembled Content: %s", fullContent)
				}

				// Output assembled function calls
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
		for scanner.Scan() {
			line := scanner.Text()

			// Log each line received from JetBrains API
			log.Printf("JetBrains API Response Line: %s", line)

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
				log.Printf("Received FunctionCall event: %v", data)
				funcNameInterface := data["name"]
				funcArgs, _ := data["content"].(string)

				var funcName string
				if funcNameInterface == nil {
					funcName = "" // This will be converted to "None" in output
				} else {
					funcName, _ = funcNameInterface.(string)
				}

				// Debug logging
				log.Printf("DEBUG: funcNameInterface = %v, funcName = '%s', funcArgs = '%s'", funcNameInterface, funcName, funcArgs)
				log.Printf("Function name: %s, Arguments: %s", funcName, funcArgs)

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
				if len(assembledContent) > 0 {
					fullContent := strings.Join(assembledContent, "")
					log.Printf("Assembled Content: %s", fullContent)
				}

				// Output assembled function calls
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

		c.JSON(http.StatusOK, response)
	}
}

func stringPtr(s string) *string {
	return &s
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
	loadClientAPIKeys()
	loadJetbrainsAccounts()

	// Create example configuration files if they don't exist
	createExampleConfigs()

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

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

	// API routes
	api := r.Group("/v1")
	api.Use(authenticateClient)
	{
		api.GET("/models", listModels)
		api.POST("/chat/completions", chatCompletions)
	}

	log.Println("Starting JetBrains AI OpenAI Compatible API server...")
	log.Println("Endpoints:")
	log.Println("  GET  /v1/models")
	log.Println("  POST /v1/chat/completions")
	log.Println("\nUse client API key in Authorization header (Bearer sk-xxx)")
	log.Println("\nConfiguration: Please edit .env file to set API keys and account information")

	if err := r.Run(":7860"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func createExampleConfigs() {
	// Create .env file if it doesn't exist
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		envContent := `# JetBrains AI API Configuration
# 客户端API密钥（逗号分隔多个）
CLIENT_API_KEYS=sk-your-custom-key-here

# JetBrains AI 账户配置（逗号分隔多个）
JETBRAINS_LICENSE_IDS=
JETBRAINS_AUTHORIZATIONS=
JETBRAINS_JWTS=your-jwt-here
`
		os.WriteFile(".env", []byte(envContent), 0644)
		log.Println("Created example .env file")
	}

	// Create models.json if it doesn't exist
	if _, err := os.Stat("models.json"); os.IsNotExist(err) {
		config := ModelsConfig{
			Models: []string{"anthropic-claude-3.5-sonnet"},
			AnthropicModelMappings: map[string]string{
				"claude-3.5-sonnet": "anthropic-claude-3.5-sonnet",
				"sonnet":            "anthropic-claude-3.5-sonnet",
			},
		}
		data, _ := json.MarshalIndent(config, "", "  ")
		os.WriteFile("models.json", data, 0644)
		log.Println("Created example models.json file")
	}
}
