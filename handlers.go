package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	setJetbrainsHeaders(req, account.JWT)

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
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("API Error: Status %d, Body: %s", resp.StatusCode, string(body))
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	if request.Stream {
		handleStreamingResponse(c, resp, request, startTime, accountIdentifier)
	} else {
		handleNonStreamingResponse(c, resp, request, startTime, accountIdentifier)
	}
}

// handleStreamingResponse 处理流式响应
func handleStreamingResponse(c *gin.Context, resp *http.Response, request ChatCompletionRequest, startTime time.Time, accountIdentifier string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	streamID := "chatcmpl-" + uuid.New().String()
	firstChunkSent := false
	var currentTool *map[string]any

	// Variables for assembling complete response content
	var assembledContent []string
	var assembledFunctionCalls []map[string]string

	scanner := bufio.NewScanner(resp.Body)
	success := true
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") || line == "data: end" {
			continue
		}

		dataStr := line[6:]
		var data map[string]any
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

			var deltaPayload map[string]any
			if !firstChunkSent {
				deltaPayload = map[string]any{
					"role":    "assistant",
					"content": content,
				}
				firstChunkSent = true
			} else {
				deltaPayload = map[string]any{
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
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(respJSON))
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
				currentTool = &map[string]any{
					"index": 0,
					"id":    fmt.Sprintf("call_%s", uuid.New().String()),
					"function": map[string]any{
						"arguments": "",
						"name":      funcName,
					},
					"type": "function",
				}
			} else if currentTool != nil {
				// Continuation of function arguments
				if funcMap, ok := (*currentTool)["function"].(map[string]any); ok {
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
				deltaPayload := map[string]any{
					"tool_calls": []map[string]any{*currentTool},
				}
				streamResp := StreamResponse{
					ID:      streamID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   request.Model,
					Choices: []StreamChoice{{Delta: deltaPayload}},
				}
				respJSON, _ := json.Marshal(streamResp)
				fmt.Fprintf(c.Writer, "data: %s\n\n", string(respJSON))
				c.Writer.Flush()
			}

			finalResp := StreamResponse{
				ID:      streamID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   request.Model,
				Choices: []StreamChoice{{Delta: map[string]any{}, FinishReason: stringPtr("tool_calls")}},
			}

			respJSON, _ := json.Marshal(finalResp)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(respJSON))
			c.Writer.Write([]byte("data: [DONE]\n\n"))
			c.Writer.Flush()
			break
		}
	}

	recordRequest(success, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
}

// handleNonStreamingResponse 处理非流式响应
func handleNonStreamingResponse(c *gin.Context, resp *http.Response, request ChatCompletionRequest, startTime time.Time, accountIdentifier string) {
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

		if !strings.HasPrefix(line, "data: ") || line == "data: end" {
			continue
		}

		dataStr := line[6:]
		var data map[string]any
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

// stringPtr 返回字符串指针
func stringPtr(s string) *string {
	return &s
}