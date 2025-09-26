package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// authenticateClient 客户端认证中间件
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

// listModels 列出可用模型
func listModels(c *gin.Context) {
	modelList := ModelList{
		Object: "list",
		Data:   modelsData.Data,
	}
	c.JSON(http.StatusOK, modelList)
}

// chatCompletions handles chat completion requests
func chatCompletions(c *gin.Context) {
	startTime := time.Now()

	// 记录性能指标开始
	defer func() {
		duration := time.Since(startTime)
		RecordHTTPRequest(duration)
	}()

	var request ChatCompletionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		recordFailureWithTimer(startTime, "", "")
		RecordHTTPError()
		respondWithError(c, http.StatusBadRequest, err.Error())
		return
	}

	modelConfig := getModelItem(request.Model)
	if modelConfig == nil {
		recordFailureWithTimer(startTime, request.Model, "")
		respondWithError(c, http.StatusNotFound, fmt.Sprintf("Model %s not found", request.Model))
		return
	}

	account, err := getNextJetbrainsAccount()
	if err != nil {
		recordFailureWithTimer(startTime, request.Model, "")
		respondWithError(c, http.StatusTooManyRequests, err.Error())
		return
	}
	defer func() {
		// Return the account to the pool when the function exits
		select {
		case accountPool <- account:
			// Returned successfully
		default:
			// Pool is full, which shouldn't happen if managed correctly.
			Warn("account pool is full. Could not return account.")
		}
	}()

	accountIdentifier := getTokenDisplayName(account)

	// Convert OpenAI format to JetBrains format with caching
	messagesCacheKey := generateMessagesCacheKey(request.Messages)
	jetbrainsMessagesAny, found := messageConversionCache.Get(messagesCacheKey)
	var jetbrainsMessages []JetbrainsMessage
	if found {
		jetbrainsMessages = jetbrainsMessagesAny.([]JetbrainsMessage)
		RecordCacheHit()
	} else {
		jetbrainsMessages = openAIToJetbrainsMessages(request.Messages)
		messageConversionCache.Set(messagesCacheKey, jetbrainsMessages, 10*time.Minute)
		RecordCacheMiss()
	}

	// CRITICAL FIX: Force tool usage when tools are provided
	if len(request.Tools) > 0 {
		if request.ToolChoice == nil {
			request.ToolChoice = "any"
			Debug("FORCING tool_choice to 'any' for tool usage guarantee")
		}
	}

	var data []JetbrainsData
	if len(request.Tools) > 0 {
		toolsCacheKey := generateToolsCacheKey(request.Tools)
		validatedToolsAny, found := toolsValidationCache.Get(toolsCacheKey)
		var validatedTools []Tool
		if found {
			validatedTools = validatedToolsAny.([]Tool)
			RecordCacheHit()
		} else {
			validationStart := time.Now()
			var validationErr error
			validatedTools, validationErr = validateAndTransformTools(request.Tools)
			validationDuration := time.Since(validationStart)
			RecordToolValidation(validationDuration)

			if validationErr != nil {
				recordFailureWithTimer(startTime, request.Model, accountIdentifier)
				RecordHTTPError()
				respondWithError(c, http.StatusBadRequest, fmt.Sprintf("Tool validation failed: %v", validationErr))
				return
			}
			toolsValidationCache.Set(toolsCacheKey, validatedTools, 30*time.Minute)
			RecordCacheMiss()
		}

		if len(validatedTools) > 0 {
			data = append(data, JetbrainsData{Type: "json", FQDN: "llm.parameters.tools"})
			// 转换为JetBrains格式
			var jetbrainsTools []JetbrainsToolDefinition
			for _, tool := range validatedTools {
				jetbrainsTools = append(jetbrainsTools, JetbrainsToolDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters: JetbrainsToolParametersWrapper{
						Schema: tool.Function.Parameters,
					},
				})
			}
			toolsJSON, marshalErr := marshalJSON(jetbrainsTools)
			if marshalErr != nil {
				recordFailureWithTimer(startTime, request.Model, accountIdentifier)
				respondWithError(c, http.StatusInternalServerError, "Failed to marshal tools")
				return
			}
			Debug("Transformed tools for JetBrains API: %s", string(toolsJSON))
			data = append(data, JetbrainsData{Type: "json", Value: string(toolsJSON)})
			// 添加modified字段，模拟preview.json的格式
			modifiedTime := time.Now().UnixMilli()
			if len(data) > 1 {
				// 为第二个data项（工具定义）添加modified字段
				lastIndex := len(data) - 1
				modifiedData := JetbrainsData{
					Type:     data[lastIndex].Type,
					Value:    data[lastIndex].Value,
					Modified: modifiedTime,
				}
				data[lastIndex] = modifiedData
			}
			if shouldForceToolUse(request) {
				jetbrainsMessages = openAIToJetbrainsMessages(request.Messages)
				Debug("Using original messages for tool usage")
			}
		}
	}
	if data == nil {
		data = []JetbrainsData{}
	}

	internalModel := getInternalModelName(request.Model)
	payload := JetbrainsPayload{
		Prompt:     "ij.chat.request.new-chat-on-start",
		Profile:    internalModel,
		Chat:       JetbrainsChat{Messages: jetbrainsMessages},
		Parameters: JetbrainsParameters{Data: data},
	}

	payloadBytes, err := marshalJSON(payload)
	if err != nil {
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		respondWithError(c, http.StatusInternalServerError, "Failed to marshal request")
		return
	}

	Debug("=== JetBrains API Request Debug ===")
	Debug("Model: %s -> %s", request.Model, internalModel)
	Debug("Messages processed: %d", len(jetbrainsMessages))
	Debug("Tools processed: %d", len(request.Tools))
	Debug("Payload size: %d bytes", len(payloadBytes))
	Debug("=== Complete Upstream Payload ===")
	Debug("%s", string(payloadBytes))
	Debug("=== End Upstream Payload ===")
	Debug("=== End Debug ===")

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/llm/chat/stream/v8", bytes.NewBuffer(payloadBytes))
	if err != nil {
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		respondWithError(c, http.StatusInternalServerError, "Failed to create request")
		return
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	setJetbrainsHeaders(req, account.JWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		respondWithError(c, http.StatusInternalServerError, "Failed to make request")
		return
	}
	defer resp.Body.Close()

	Debug("JetBrains API Response Status: %d", resp.StatusCode)

	if resp.StatusCode == 477 {
		Warn("Account %s has no quota (received 477)", getTokenDisplayName(account))
		account.HasQuota = false
		account.LastQuotaCheck = float64(time.Now().Unix())
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errorMsg := string(body)
		Error("JetBrains API Error: Status %d, Body: %s", resp.StatusCode, errorMsg)
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		c.JSON(resp.StatusCode, gin.H{"error": errorMsg})
		return
	}

	if request.Stream {
		handleStreamingResponse(c, resp, request, startTime, accountIdentifier)
	} else {
		handleNonStreamingResponse(c, resp, request, startTime, accountIdentifier)
	}
}
