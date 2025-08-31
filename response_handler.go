package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// generateShortToolCallID generates a tool call ID that fits JetBrains 40-char limit
func generateShortToolCallID() string {
	// Generate 16 random bytes and encode as hex (32 chars) + "call_" prefix (5 chars) = 37 chars total
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("call_%s", hex.EncodeToString(bytes))
}

// processJetbrainsStream processes the event stream from the JetBrains API.
// It calls the provided onEvent function for each event in the stream.
func processJetbrainsStream(resp *http.Response, onEvent func(event map[string]any) bool) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") || line == "data: end" {
			continue
		}

		dataStr := line[6:]
		var data map[string]any
		if err := sonic.Unmarshal([]byte(dataStr), &data); err != nil {
			log.Printf("Error unmarshalling stream event: %v", err)
			continue
		}

		if !onEvent(data) {
			break
		}
	}
}

// handleStreamingResponse handles streaming responses from the JetBrains API
func handleStreamingResponse(c *gin.Context, resp *http.Response, request ChatCompletionRequest, startTime time.Time, accountIdentifier string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	streamID := "chatcmpl-" + uuid.New().String()
	firstChunkSent := false
	var currentTool *map[string]any

	processJetbrainsStream(resp, func(data map[string]any) bool {
		eventType, _ := data["type"].(string)

		switch eventType {
		case "Content":
			content, _ := data["content"].(string)
			if content == "" {
				return true // Continue processing
			}

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

			respJSON, _ := marshalJSON(streamResp)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(respJSON))
			c.Writer.Flush()
		case "ToolCall":
			// 处理新的ToolCall格式
			if name, ok := data["name"].(string); ok && name != "" {
				// 开始新的工具调用
				currentTool = &map[string]any{
					"index": 0,
					"id":    generateShortToolCallID(),
					"function": map[string]any{
						"arguments": "",
						"name":      name,
					},
					"type": "function",
				}
			} else if currentTool != nil {
				// 累积参数内容
				if content, ok := data["content"].(string); ok {
					if funcMap, ok := (*currentTool)["function"].(map[string]any); ok {
						currentArgs, _ := funcMap["arguments"].(string)
						funcMap["arguments"] = currentArgs + content
					}
				}
			}
		case "FunctionCall":
			funcNameInterface := data["name"]
			funcArgs, _ := data["content"].(string)

			var funcName string
			if funcNameInterface == nil {
				funcName = ""
			} else {
				funcName, _ = funcNameInterface.(string)
			}

			if funcName != "" {
				currentTool = &map[string]any{
					"index": 0,
					"id":    generateShortToolCallID(),
					"function": map[string]any{
						"arguments": "",
						"name":      funcName,
					},
					"type": "function",
				}
			} else if currentTool != nil {
				if funcMap, ok := (*currentTool)["function"].(map[string]any); ok {
					currentArgs, _ := funcMap["arguments"].(string)
					funcMap["arguments"] = currentArgs + funcArgs
				}
			}
		case "FinishMetadata":
			if currentTool != nil {
				// Validate the tool call arguments before sending
				if funcMap, ok := (*currentTool)["function"].(map[string]any); ok {
					if args, ok := funcMap["arguments"].(string); ok && args != "" {
						// Try to validate JSON format
						var argsTest map[string]any
						if err := sonic.Unmarshal([]byte(args), &argsTest); err != nil {
							log.Printf("Warning: Tool call arguments are not valid JSON: %v", err)
						}
					}
				}

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
				respJSON, _ := marshalJSON(streamResp)
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

			respJSON, _ := marshalJSON(finalResp)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(respJSON))
			c.Writer.Write([]byte("data: [DONE]\n\n"))
			c.Writer.Flush()
			return false // Stop processing
		}
		return true // Continue processing
	})

	recordRequest(true, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
}

// handleNonStreamingResponse handles non-streaming responses from the JetBrains API
func handleNonStreamingResponse(c *gin.Context, resp *http.Response, request ChatCompletionRequest, startTime time.Time, accountIdentifier string) {
	var contentBuilder strings.Builder
	var toolCalls []ToolCall
	var currentFuncName string
	var currentFuncArgs string

	processJetbrainsStream(resp, func(data map[string]any) bool {
		eventType, _ := data["type"].(string)

		switch eventType {
		case "Content":
			if content, ok := data["content"].(string); ok {
				contentBuilder.WriteString(content)
			}
		case "ToolCall":
			// 处理新的ToolCall格式
			if name, ok := data["name"].(string); ok && name != "" {
				// 开始新的工具调用
				currentFuncName = name
				currentFuncArgs = ""
			} else if content, ok := data["content"].(string); ok {
				// 累积参数内容
				currentFuncArgs += content
			}
		case "FunctionCall":
			funcNameInterface := data["name"]
			funcArgs, _ := data["content"].(string)

			var funcName string
			if funcNameInterface == nil {
				funcName = ""
			} else {
				funcName, _ = funcNameInterface.(string)
			}

			if funcName != "" {
				currentFuncName = funcName
				currentFuncArgs = ""
			}
			currentFuncArgs += funcArgs
		case "FinishMetadata":
			if currentFuncName != "" {
				toolCall := ToolCall{
					ID:   generateShortToolCallID(),
					Type: "function",
					Function: Function{
						Name:      currentFuncName,
						Arguments: currentFuncArgs,
					},
				}

				// Validate the tool call before adding it
				if err := validateToolCallResponse(toolCall); err != nil {
					log.Printf("Warning: Invalid tool call response: %v", err)
					// Still add it but log the issue
				}

				toolCalls = append(toolCalls, toolCall)
			}
			return false // Stop processing
		}
		return true // Continue processing
	})

	message := ChatMessage{
		Role:    "assistant",
		Content: contentBuilder.String(),
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

	recordRequest(true, time.Since(startTime).Milliseconds(), request.Model, accountIdentifier)
	c.JSON(http.StatusOK, response)
}

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}
