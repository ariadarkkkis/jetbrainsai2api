package main

import (
	"log"
)

// openAIToJetbrainsMessages converts OpenAI chat messages to JetBrains format
func openAIToJetbrainsMessages(messages []ChatMessage) ([]JetbrainsMessage, map[string]string) {
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
	return jetbrainsMessages, toolIDToFuncNameMap
}

// convertAnthropicToOpenAI converts Anthropic message format to OpenAI format
func convertAnthropicToOpenAI(request AnthropicMessageRequest) (ChatCompletionRequest, error) {
	var openAIRequest ChatCompletionRequest

	openAIRequest.Model = request.Model
	openAIRequest.Stream = request.Stream
	openAIRequest.Temperature = request.Temperature
	openAIRequest.TopP = request.TopP

	if request.MaxTokens > 0 {
		openAIRequest.MaxTokens = &request.MaxTokens
	}

	// Convert messages
	for _, msg := range request.Messages {
		openAIMsg := ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
		openAIRequest.Messages = append(openAIRequest.Messages, openAIMsg)
	}

	// Convert tools if present
	if request.Tools != nil {
		for _, tool := range request.Tools {
			openAITool := Tool{
				Type: "function",
				Function: ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			}
			openAIRequest.Tools = append(openAIRequest.Tools, openAITool)
		}
	}

	return openAIRequest, nil
}

