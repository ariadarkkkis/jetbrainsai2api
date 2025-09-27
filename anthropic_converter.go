package main

import (
	"fmt"
	"strings"
	"time"
)

// anthropicToOpenAIRequest 将 Anthropic 请求转换为 OpenAI 格式
// 应用 DRY 原则：复用现有的 OpenAI -> JetBrains 转换逻辑
func anthropicToOpenAIRequest(anthReq *AnthropicMessagesRequest) (*ChatCompletionRequest, error) {
	Debug("Converting Anthropic request to OpenAI format")

	// 转换消息格式 (KISS: 保持简单的映射逻辑)
	var openAIMessages []ChatMessage

	// 处理系统消息 - Anthropic 使用单独的 system 字段
	if anthReq.System != "" {
		openAIMessages = append(openAIMessages, ChatMessage{
			Role:    "system",
			Content: string(anthReq.System), // 将 FlexibleString 转换为 string
		})
	}

	// 转换用户和助手消息
	for _, msg := range anthReq.Messages {
		openAIMsg := ChatMessage{
			Role: msg.Role,
		}

		// 处理内容 - 支持多种格式 (SRP: 单一职责处理内容转换)
		content, err := convertAnthropicContent(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message content: %w", err)
		}
		openAIMsg.Content = content

		openAIMessages = append(openAIMessages, openAIMsg)
	}

	// 转换工具定义 (DRY: 复用现有工具转换逻辑)
	var tools []Tool
	if len(anthReq.Tools) > 0 {
		for _, anthTool := range anthReq.Tools {
			tools = append(tools, Tool{
				Type: "function",
				Function: ToolFunction{
					Name:        anthTool.Name,
					Description: anthTool.Description,
					Parameters:  anthTool.InputSchema,
				},
			})
		}
	}

	// 构建 OpenAI 请求
	openAIReq := &ChatCompletionRequest{
		Model:       anthReq.Model,
		Messages:    openAIMessages,
		Stream:      anthReq.Stream != nil && *anthReq.Stream,
		Temperature: anthReq.Temperature,
		TopP:        anthReq.TopP,
		Tools:       tools,
		ToolChoice:  anthReq.ToolChoice,
	}

	// 处理 MaxTokens - Anthropic 必填，OpenAI 可选
	if anthReq.MaxTokens > 0 {
		maxTokens := anthReq.MaxTokens
		openAIReq.MaxTokens = &maxTokens
	}

	// 处理停止序列
	if len(anthReq.StopSequences) > 0 {
		if len(anthReq.StopSequences) == 1 {
			openAIReq.Stop = anthReq.StopSequences[0]
		} else {
			openAIReq.Stop = anthReq.StopSequences
		}
	}

	Debug("Successfully converted Anthropic request: model=%s, messages=%d, tools=%d",
		openAIReq.Model, len(openAIReq.Messages), len(openAIReq.Tools))

	return openAIReq, nil
}

// convertAnthropicContent 转换 Anthropic 内容格式到 OpenAI 格式
// SRP: 专门处理内容转换的单一职责
func convertAnthropicContent(content any) (any, error) {
	switch v := content.(type) {
	case string:
		// 简单文本内容
		return v, nil

	case []any:
		// 复杂内容块数组
		var result []map[string]any
		var textParts []string

		for _, block := range v {
			blockMap, ok := block.(map[string]any)
			if !ok {
				continue
			}

			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "text":
				if text, ok := blockMap["text"].(string); ok {
					textParts = append(textParts, text)
				}
			case "image":
				// 转换图像内容 (保持与现有图像处理兼容)
				if source, ok := blockMap["source"].(map[string]any); ok {
					imageContent := map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": source["data"], // 或 source["url"]
						},
					}
					result = append(result, imageContent)
				}
			}
		}

		// 如果只有文本，返回合并的字符串 (KISS: 简化处理)
		if len(result) == 0 && len(textParts) > 0 {
			return strings.Join(textParts, "\n"), nil
		}

		// 如果有混合内容，返回结构化格式
		if len(textParts) > 0 {
			textContent := map[string]any{
				"type": "text",
				"text": strings.Join(textParts, "\n"),
			}
			result = append([]map[string]any{textContent}, result...)
		}

		return result, nil

	default:
		// 其他格式直接返回
		return content, nil
	}
}

// openAIToAnthropicResponse 将 OpenAI 响应转换为 Anthropic 格式
// DIP: 依赖抽象的响应结构而非具体实现
func openAIToAnthropicResponse(openAIResp *ChatCompletionResponse) (*AnthropicMessagesResponse, error) {
	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := openAIResp.Choices[0]

	// 转换内容格式
	var content []AnthropicContentBlock

	switch v := choice.Message.Content.(type) {
	case string:
		if v != "" {
			content = append(content, AnthropicContentBlock{
				Type: "text",
				Text: v,
			})
		}

	case []any:
		// 处理结构化内容
		for _, item := range v {
			if itemMap, ok := item.(map[string]any); ok {
				if itemType, _ := itemMap["type"].(string); itemType == "text" {
					if text, _ := itemMap["text"].(string); text != "" {
						content = append(content, AnthropicContentBlock{
							Type: "text",
							Text: text,
						})
					}
				}
			}
		}
	}

	// 构建 Anthropic 响应
	anthResp := &AnthropicMessagesResponse{
		ID:         openAIResp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      openAIResp.Model,
		StopReason: mapFinishReason(choice.FinishReason),
		Usage: AnthropicUsage{
			InputTokens:  getIntValue(openAIResp.Usage, "prompt_tokens"),
			OutputTokens: getIntValue(openAIResp.Usage, "completion_tokens"),
		},
	}

	Debug("Converted OpenAI response to Anthropic format: id=%s, content_blocks=%d",
		anthResp.ID, len(anthResp.Content))

	return anthResp, nil
}

// mapFinishReason 映射结束原因 (KISS: 简单映射表)
func mapFinishReason(openAIReason string) string {
	switch openAIReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// getIntValue 安全获取整数值 (DRY: 复用的工具函数)
func getIntValue(usage map[string]int, key string) int {
	if usage == nil {
		return 0
	}
	return usage[key]
}

// generateAnthropicStreamResponse 生成流式响应
// OCP: 开放扩展，支持不同的流式格式
func generateAnthropicStreamResponse(responseType string, content string, index int) []byte {
	var resp AnthropicStreamResponse

	switch responseType {
	case "content_block_start":
		resp = AnthropicStreamResponse{
			Type:  "content_block_start",
			Index: &index,
		}

	case "content_block_delta":
		resp = AnthropicStreamResponse{
			Type:  "content_block_delta",
			Index: &index,
			Delta: &struct {
				Type string `json:"type,omitempty"`
				Text string `json:"text,omitempty"`
			}{
				Type: "text_delta",
				Text: content,
			},
		}

	case "content_block_stop":
		resp = AnthropicStreamResponse{
			Type:  "content_block_stop",
			Index: &index,
		}

	case "message_start":
		resp = AnthropicStreamResponse{
			Type: "message_start",
			Message: &AnthropicMessagesResponse{
				ID:   generateMessageID(),
				Type: "message",
				Role: "assistant",
				Usage: AnthropicUsage{
					InputTokens:  0,
					OutputTokens: 0,
				},
			},
		}

	case "message_stop":
		resp = AnthropicStreamResponse{
			Type: "message_stop",
		}

	default:
		resp = AnthropicStreamResponse{
			Type: "error",
		}
	}

	data, _ := marshalJSON(resp)
	return data
}

// generateMessageID 生成消息 ID (KISS: 简单的 ID 生成)
func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
