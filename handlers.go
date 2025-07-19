package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
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

// chatCompletions 处理聊天完成请求
func chatCompletions(c *gin.Context) {
	startTime := time.Now()
	var request ChatCompletionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		recordFailureWithTimer(startTime, "", "")
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

	accountIdentifier := getTokenDisplayName(account)

	// Convert OpenAI format to JetBrains format
	jetbrainsMessages := openAIToJetbrainsMessages(request.Messages)

	var data []JetbrainsData
	var tools []ToolFunction
	if request.Tools != nil {
		data = append(data, JetbrainsData{Type: "json", FQDN: "llm.parameters.functions"})
		for _, tool := range request.Tools {
			tools = append(tools, tool.Function)
		}
		toolsJSON, _ := marshalJSON(tools)
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

	payloadBytes, err := marshalJSON(payload)
	if err != nil {
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		respondWithError(c, http.StatusInternalServerError, "Failed to marshal request")
		return
	}

	if gin.Mode() == gin.DebugMode {
		log.Printf("Sending payload to JetBrains API: %s", string(payloadBytes))
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/llm/chat/stream/v7", bytes.NewBuffer(payloadBytes))
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

	// Output JetBrains API response status and headers
	if gin.Mode() == gin.DebugMode {
		log.Printf("JetBrains API Response Status: %d", resp.StatusCode)
		log.Printf("JetBrains API Response Headers: %+v", resp.Header)
	}

	if resp.StatusCode == 477 {
		log.Printf("Account %s has no quota (received 477)", getTokenDisplayName(account))
		account.HasQuota = false
		account.LastQuotaCheck = float64(time.Now().Unix())
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("API Error: Status %d, Body: %s", resp.StatusCode, string(body))
		recordFailureWithTimer(startTime, request.Model, accountIdentifier)
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	if request.Stream {
		handleStreamingResponse(c, resp, request, startTime, accountIdentifier)
	} else {
		handleNonStreamingResponse(c, resp, request, startTime, accountIdentifier)
	}
}

