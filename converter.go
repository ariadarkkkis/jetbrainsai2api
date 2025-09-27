package main

import (
	"github.com/bytedance/sonic"
)

// openAIToJetbrainsMessages converts OpenAI chat messages to JetBrains format
func openAIToJetbrainsMessages(messages []ChatMessage) []JetbrainsMessage {
	toolIDToFuncNameMap := make(map[string]string)
	validator := NewImageValidator()

	for _, msg := range messages {
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolIDToFuncNameMap[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	var jetbrainsMessages []JetbrainsMessage
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			// Check for image content in user messages
			mediaType, imageData, hasImage := ExtractImageDataFromContent(msg.Content)
			if hasImage {
				// Validate the image
				if err := validator.ValidateImageData(mediaType, imageData); err != nil {
					Warn("Image validation failed: %v", err)
					// Continue with text content only if image validation fails
					textContent := extractTextContent(msg.Content)
					jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
						Type:    "user_message",
						Content: textContent,
					})
				} else {
					// Add image message for v8 API
					jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
						Type:      "media_message",
						MediaType: mediaType,
						Data:      imageData,
					})

					// Add text message if there's also text content
					textContent := extractTextContent(msg.Content)
					if textContent != "" {
						jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
							Type:    "user_message",
							Content: textContent,
						})
					}
				}
			} else {
				// Handle multiple text content blocks separately
				if contentArray, ok := msg.Content.([]any); ok {
					for _, item := range contentArray {
						if itemMap, ok := item.(map[string]any); ok {
							if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
								if text, ok := itemMap["text"].(string); ok && text != "" {
									jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
										Type:    "user_message",
										Content: text,
									})
								}
							}
						}
					}
				} else {
					// Single text content
					textContent := extractTextContent(msg.Content)
					jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
						Type:    "user_message",
						Content: textContent,
					})
				}
			}
		case "system":
			textContent := extractTextContent(msg.Content)
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    "system_message",
				Content: textContent,
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// V8 API: Use assistant_message_tool for tool calls
				toolCall := msg.ToolCalls[0]

				// 尝试解析参数，如果是一个 JSON 字符串，就解码它以获取原始的参数对象
				var argsMap map[string]any
				if err := sonic.UnmarshalString(toolCall.Function.Arguments, &argsMap); err == nil {
					// 如果成功解码，重新编码以确保它是一个干净的 JSON
					cleanArgs, _ := marshalJSON(argsMap)
					toolCall.Function.Arguments = string(cleanArgs)
				}

				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:     "assistant_message_tool",
					ID:       toolCall.ID,
					ToolName: toolCall.Function.Name,
					Content:  toolCall.Function.Arguments,
				})
			} else {
				// V8 API: Use assistant_message_text for text responses
				textContent := extractTextContent(msg.Content)
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:    "assistant_message_text",
					Content: textContent,
				})
			}
		case "tool":
			functionName := toolIDToFuncNameMap[msg.ToolCallID]
			if functionName != "" {
				// V8 API: Use tool_message for tool results
				textContent := extractTextContent(msg.Content)
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:     "tool_message",
					ID:       msg.ToolCallID,
					ToolName: functionName,
					Result:   textContent,
				})
			} else {
				Warn("Cannot find function name for tool_call_id %s", msg.ToolCallID)
			}
		default:
			textContent := extractTextContent(msg.Content)
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    "user_message",
				Content: textContent,
			})
		}
	}
	return jetbrainsMessages
}
