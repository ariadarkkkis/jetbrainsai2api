package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

// handleAnthropicStreamingResponse 处理流式响应 (Anthropic 格式)
// SRP: 专门处理 Anthropic 流式响应的单一职责
func handleAnthropicStreamingResponse(c *gin.Context, resp *http.Response, anthReq *AnthropicMessagesRequest, startTime time.Time, accountIdentifier string) {
	defer resp.Body.Close()

	// 设置 Anthropic 流式响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 发送 message_start 事件
	messageStartData := generateAnthropicStreamResponse("message_start", "", 0)
	c.Writer.Write([]byte("event: message_start\n"))
	c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(messageStartData))))
	c.Writer.Flush()

	// 发送 content_block_start 事件
	contentBlockStartData := generateAnthropicStreamResponse("content_block_start", "", 0)
	c.Writer.Write([]byte("event: content_block_start\n"))
	c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(contentBlockStartData))))
	c.Writer.Flush()

	scanner := bufio.NewScanner(resp.Body)
	var fullContent strings.Builder
	var hasContent bool
	lineCount := 0

	Debug("=== JetBrains Streaming Response Debug ===")

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		// 记录每一行原始数据
		Debug("Line %d: '%s'", lineCount, line)

		line = strings.TrimSpace(line)

		if line == "" {
			Debug("Line %d: Empty line, skipping", lineCount)
			continue
		}

		// 处理 SSE 格式 (KISS: 简单的行解析)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			Debug("Line %d: SSE data = '%s'", lineCount, data)

			if data == "[DONE]" {
				Debug("Line %d: Found [DONE], breaking", lineCount)
				break
			}
			if data == "end" {
				Debug("Line %d: Found 'end', breaking", lineCount)
				break
			}

			// 解析 JetBrains 流式数据
			content, err := parseJetbrainsStreamData(data)
			if err != nil {
				Debug("Line %d: Failed to parse stream data: %v", lineCount, err)
				continue
			}

			Debug("Line %d: Parsed content = '%s'", lineCount, content)

			if content != "" {
				hasContent = true
				fullContent.WriteString(content)

				// 发送 content_block_delta 事件 (Anthropic 格式)
				contentBlockDeltaData := generateAnthropicStreamResponse("content_block_delta", content, 0)

				// 检查连接状态
				select {
				case <-c.Request.Context().Done():
					Debug("Line %d: Client disconnected during streaming, stopping", lineCount)
					return
				default:
					// 连接正常，继续发送
				}

				bytesWritten, err := c.Writer.Write([]byte("event: content_block_delta\n"))
				if err != nil {
					Debug("Line %d: Failed to write event header: %v", lineCount, err)
					return
				}
				Debug("Line %d: Wrote event header, %d bytes", lineCount, bytesWritten)

				dataLine := fmt.Sprintf("data: %s\n\n", string(contentBlockDeltaData))
				bytesWritten, err = c.Writer.Write([]byte(dataLine))
				if err != nil {
					Debug("Line %d: Failed to write data: %v", lineCount, err)
					return
				}
				Debug("Line %d: Wrote data, %d bytes, content: '%s'", lineCount, bytesWritten, content)

				if flusher, ok := c.Writer.(http.Flusher); ok {
					flusher.Flush()
					Debug("Line %d: Flushed data to client", lineCount)
				} else {
					Debug("Line %d: Warning: Writer does not support flushing", lineCount)
				}
			}
		} else {
			Debug("Line %d: Not SSE data format, raw line: '%s'", lineCount, line)
		}
	}

	Debug("=== Streaming Response Summary ===")
	Debug("Total lines processed: %d", lineCount)
	Debug("Has content: %v", hasContent)
	Debug("Full aggregated content: '%s'", fullContent.String())
	Debug("===================================")

	// 发送 content_block_stop 事件
	contentBlockStopData := generateAnthropicStreamResponse("content_block_stop", "", 0)
	c.Writer.Write([]byte("event: content_block_stop\n"))
	c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(contentBlockStopData))))
	c.Writer.Flush()

	// 发送 message_stop 事件
	messageStopData := generateAnthropicStreamResponse("message_stop", "", 0)
	c.Writer.Write([]byte("event: message_stop\n"))
	c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", string(messageStopData))))
	c.Writer.Flush()

	if hasContent {
		recordSuccess(startTime, anthReq.Model, accountIdentifier)
		Debug("Anthropic streaming response completed successfully")
	} else {
		recordFailureWithTimer(startTime, anthReq.Model, accountIdentifier)
		Warn("Anthropic streaming response completed with no content")
	}
}

// handleAnthropicNonStreamingResponse 处理非流式响应 (Anthropic 格式)
// SRP: 专门处理 Anthropic 非流式响应的单一职责
func handleAnthropicNonStreamingResponse(c *gin.Context, resp *http.Response, anthReq *AnthropicMessagesRequest, startTime time.Time, accountIdentifier string) {
	defer resp.Body.Close()

	// 读取完整响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		recordFailureWithTimer(startTime, anthReq.Model, accountIdentifier)
		respondWithAnthropicError(c, http.StatusInternalServerError, "api_error",
			"Failed to read response body")
		return
	}

	Debug("JetBrains API Response Body: %s", string(body))

	// 直接转换 JetBrains 响应为 Anthropic 格式 (KISS: 消除中间转换)
	anthResp, err := parseJetbrainsToAnthropicDirect(body, anthReq.Model)
	if err != nil {
		recordFailureWithTimer(startTime, anthReq.Model, accountIdentifier)
		respondWithAnthropicError(c, http.StatusInternalServerError, "api_error",
			fmt.Sprintf("Failed to parse response: %v", err))
		return
	}

	recordSuccess(startTime, anthReq.Model, accountIdentifier)
	c.JSON(http.StatusOK, anthResp)

	Debug("Anthropic non-streaming response completed successfully: id=%s", anthResp.ID)
}

// parseJetbrainsStreamData 解析 JetBrains 流式数据
// KISS: 保持简单的解析逻辑
func parseJetbrainsStreamData(data string) (string, error) {
	if data == "" || data == "null" {
		return "", nil
	}

	// 尝试解析 JSON 数据
	var streamData map[string]any
	if err := sonic.Unmarshal([]byte(data), &streamData); err != nil {
		// 如果不是 JSON，可能是纯文本
		return data, nil
	}

	// 提取内容：优先处理 JetBrains API 格式
	if eventType, ok := streamData["type"].(string); ok && eventType == "Content" {
		if content, ok := streamData["content"].(string); ok {
			return content, nil
		}
	}

	// 兼容 OpenAI 格式 (保留原有逻辑)
	if choices, ok := streamData["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if delta, ok := choice["delta"].(map[string]any); ok {
				if content, ok := delta["content"].(string); ok {
					return content, nil
				}
			}
		}
	}

	// 检查是否是直接的内容响应
	if content, ok := streamData["content"].(string); ok {
		return content, nil
	}

	return "", nil
}

// parseJetbrainsNonStreamResponse 解析 JetBrains 非流式响应
// 兼容处理：JetBrains API 总是返回流式格式，需要聚合数据
func parseJetbrainsNonStreamResponse(body []byte, model string) (*ChatCompletionResponse, error) {
	bodyStr := string(body)

	// 检查是否是流式响应格式 (以 "data:" 开头)
	if strings.HasPrefix(strings.TrimSpace(bodyStr), "data:") {
		return parseAndAggregateStreamResponse(bodyStr, model)
	}

	// 尝试解析为完整的聊天响应 (保留原有逻辑)
	var jetbrainsResp map[string]any
	if err := sonic.Unmarshal(body, &jetbrainsResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// 提取内容
	var content string
	if contentField, exists := jetbrainsResp["content"]; exists {
		if contentStr, ok := contentField.(string); ok {
			content = contentStr
		} else if contentArray, ok := contentField.([]any); ok {
			// 处理数组格式的内容
			var contentParts []string
			for _, part := range contentArray {
				if partStr, ok := part.(string); ok {
					contentParts = append(contentParts, partStr)
				}
			}
			content = strings.Join(contentParts, "")
		}
	}

	// 构建 OpenAI 格式响应 (DRY: 复用响应构建逻辑)
	openAIResp := &ChatCompletionResponse{
		ID:      generateResponseID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChoice{
			{
				Message: ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				Index:        0,
				FinishReason: "stop",
			},
		},
		Usage: map[string]int{
			"prompt_tokens":     estimateTokenCount(content) / 4, // 粗略估算
			"completion_tokens": estimateTokenCount(content),
			"total_tokens":      estimateTokenCount(content) * 5 / 4,
		},
	}

	return openAIResp, nil
}

// generateResponseID 生成响应 ID (KISS: 简单的 ID 生成)
func generateResponseID() string {
	return fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
}

// estimateTokenCount 估算 token 数量 (KISS: 简单估算)
func estimateTokenCount(text string) int {
	// 简单估算：平均每个 token 约 4 个字符
	return len(text) / 4
}

// createJetbrainsStreamRequest 创建 JetBrains API 流式请求 (DRY: 提取公共逻辑)
func createJetbrainsStreamRequest(payloadBytes []byte, jwt string) (*http.Request, error) {
	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/llm/chat/stream/v8",
		strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	setJetbrainsHeaders(req, jwt)

	return req, nil
}

// parseAndAggregateStreamResponse 解析并聚合流式响应数据
// 处理 JetBrains API 的流式格式，聚合所有内容片段
func parseAndAggregateStreamResponse(bodyStr, model string) (*ChatCompletionResponse, error) {
	lines := strings.Split(bodyStr, "\n")
	var contentParts []string
	var finishReason string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 跳过空行和结束标记
		if line == "" || line == "data: end" {
			continue
		}

		// 处理 "data: " 前缀的行
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")

			// 解析 JSON 数据
			var streamData map[string]any
			if err := sonic.Unmarshal([]byte(jsonData), &streamData); err != nil {
				Debug("Failed to parse stream JSON: %v, data: %s", err, jsonData)
				continue
			}

			// 提取内容类型
			eventType, _ := streamData["type"].(string)

			switch eventType {
			case "Content":
				// 提取内容片段
				if content, ok := streamData["content"].(string); ok {
					contentParts = append(contentParts, content)
				}
			case "FinishMetadata":
				// 提取结束原因
				if reason, ok := streamData["reason"].(string); ok {
					finishReason = reason
				}
			}
		}
	}

	// 聚合所有内容片段
	fullContent := strings.Join(contentParts, "")

	if finishReason == "" {
		finishReason = "stop" // 默认结束原因
	}

	// 构建完整的 OpenAI 格式响应
	response := &ChatCompletionResponse{
		ID:      generateResponseID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: fullContent,
				},
				FinishReason: finishReason,
			},
		},
		Usage: map[string]int{
			"prompt_tokens":     0, // JetBrains API 通常不返回 token 计数
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}

	Debug("Successfully aggregated stream response: %d content parts, finish_reason=%s",
		len(contentParts), finishReason)

	return response, nil
}