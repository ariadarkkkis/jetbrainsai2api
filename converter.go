package main

import (
	"log"

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
		textContent := extractTextContent(msg.Content)

		switch msg.Role {
		case "user":
			// Check for image content in user messages
			mediaType, imageData, hasImage := ExtractImageDataFromContent(msg.Content)
			if hasImage {
				// Validate the image
				if err := validator.ValidateImageData(mediaType, imageData); err != nil {
					log.Printf("Image validation failed: %v", err)
					// Continue with text content only if image validation fails
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
					if textContent != "" {
						jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
							Type:    "user_message",
							Content: textContent,
						})
					}
				}
			} else {
				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:    "user_message",
					Content: textContent,
				})
			}
		case "system":
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    "system_message",
				Content: textContent,
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				toolCall := msg.ToolCalls[0]

				// 尝试解析参数，如果是一个 JSON 字符串，就解码它以获取原始的参数对象
				var argsMap map[string]any
				if err := sonic.UnmarshalString(toolCall.Function.Arguments, &argsMap); err == nil {
					// 如果成功解码，重新编码以确保它是一个干净的 JSON
					cleanArgs, _ := marshalJSON(argsMap)
					toolCall.Function.Arguments = string(cleanArgs)
				}
				// 如果解码失败，我们假定它已经是我们想要的格式

				jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
					Type:    "assistant_message",
					Content: textContent,
					FunctionCall: &JetbrainsFunctionCall{
						FunctionName: toolCall.Function.Name,
						Content:      toolCall.Function.Arguments,
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
	return jetbrainsMessages
}
