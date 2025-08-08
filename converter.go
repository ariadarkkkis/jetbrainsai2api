package main

import (
	"github.com/bytedance/sonic"
	"log"
)

// openAIToJetbrainsMessages converts OpenAI chat messages to JetBrains format
func openAIToJetbrainsMessages(messages []ChatMessage) []JetbrainsMessage {
	toolIDToFuncNameMap := make(map[string]string)
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
		case "user", "system":
			jetbrainsMessages = append(jetbrainsMessages, JetbrainsMessage{
				Type:    msg.Role + "_message",
				Content: textContent,
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				toolCall := msg.ToolCalls[0]

				// 尝试解析参数，如果是一个 JSON 字符串，就解码它以获取原始的参数对象
				var argsMap map[string]any
				if err := sonic.UnmarshalString(toolCall.Function.Arguments, &argsMap); err == nil {
					// 如果成功解码，重新编码以确保它是一个干净的 JSON
					cleanArgs, _ := sonic.Marshal(argsMap)
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
