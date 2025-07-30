package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

const (
	// JetBrains API parameter name constraints
	MaxParamNameLength = 64
	ParamNamePattern   = "^[a-zA-Z0-9_.-]{1,64}$"
)

var paramNameRegex = regexp.MustCompile(ParamNamePattern)

// validateAndTransformTools 验证并转换工具定义以符合JetBrains API要求
func validateAndTransformTools(tools []Tool) ([]Tool, error) {
	if len(tools) == 0 {
		return tools, nil
	}

	validatedTools := make([]Tool, 0, len(tools))

	for _, tool := range tools {
		// 验证工具名称
		if !isValidParamName(tool.Function.Name) {
			log.Printf("Invalid tool name: %s, skipping tool", tool.Function.Name)
			continue
		}

		// 验证和转换参数
		transformedParams, err := transformParameters(tool.Function.Parameters)
		if err != nil {
			log.Printf("Failed to transform tool %s parameters: %v", tool.Function.Name, err)
			continue
		}

		// 创建新的工具对象
		validatedTool := Tool{
			Type: tool.Type,
			Function: ToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  transformedParams,
			},
		}

		validatedTools = append(validatedTools, validatedTool)
	}

	log.Printf("Transformed tools for JetBrains API: %s", toJSONString(validatedTools))
	return validatedTools, nil
}

// toJSONString 将对象转换为JSON字符串，用于日志记录
func toJSONString(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(data)
}

// shouldForceToolUse determines if we should force tool usage based on various factors
func shouldForceToolUse(request ChatCompletionRequest) bool {
	// Always try to encourage tool use if tools are provided
	if len(request.Tools) > 0 {
		return true
	}

	// Check tool_choice parameter
	if request.ToolChoice != nil {
		switch choice := request.ToolChoice.(type) {
		case string:
			return choice == "required" || choice == "any" || choice == "auto"
		case map[string]any:
			if choiceType, ok := choice["type"].(string); ok {
				return choiceType == "function" || choiceType == "required"
			}
		}
	}

	return false
}

// enhancePromptForToolUse enhances the user prompt to encourage tool usage
func enhancePromptForToolUse(messages []ChatMessage, tools []Tool) []ChatMessage {
	if len(messages) == 0 || len(tools) == 0 {
		return messages
	}

	// Get the last user message
	lastUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIndex = i
			break
		}
	}

	if lastUserIndex == -1 {
		return messages
	}

	// Create enhanced messages
	enhancedMessages := make([]ChatMessage, len(messages))
	copy(enhancedMessages, messages)

	// Build tool usage hint based on tool types
	originalContent := extractTextContent(messages[lastUserIndex].Content)

	// Create a more specific and compelling instruction for JetBrains AI
	var toolDescriptions []string
	for _, tool := range tools {
		toolDescriptions = append(toolDescriptions, fmt.Sprintf("'%s'(%s)", tool.Function.Name, tool.Function.Description))
	}

	// Create even more explicit and varied instructions to overcome JetBrains AI reluctance
	enhancedContent := fmt.Sprintf(`%s

CRITICAL SYSTEM INSTRUCTION: You MUST use one of the available function tools to respond to this request. Do not provide a text-only response.

Available function tools: %s

REQUIREMENTS:
1. Analyze the user request carefully
2. Select the most appropriate tool from the list above
3. Call the selected tool with appropriate parameters
4. For nested object parameters, provide them as properly formatted JSON strings
5. For enum parameters, use only values from the allowed enum list
6. Do NOT respond with explanatory text instead of using tools

This is a function calling context - tool usage is mandatory, not optional.`,
		originalContent,
		strings.Join(toolDescriptions, ", "),
	)

	enhancedMessages[lastUserIndex].Content = enhancedContent

	return enhancedMessages
}

// validateAndTransformTool validates and transforms a single tool function
func validateAndTransformTool(tool ToolFunction) (ToolFunction, error) {
	// Validate function name
	if !isValidParamName(tool.Name) {
		return ToolFunction{}, fmt.Errorf("function name '%s' doesn't match required pattern", tool.Name)
	}

	// Transform parameters
	transformedParams, err := transformParameters(tool.Parameters)
	if err != nil {
		return ToolFunction{}, fmt.Errorf("parameter transformation failed: %v", err)
	}

	return ToolFunction{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  transformedParams,
	}, nil
}

// transformParameters transforms complex parameter schemas to JetBrains-compatible format
func transformParameters(params map[string]any) (map[string]any, error) {
	if params == nil {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		}, nil
	}

	// Handle the parameters object
	result := make(map[string]any)

	// Copy basic schema properties
	if schemaType, ok := params["type"]; ok {
		result["type"] = schemaType
	}

	// Transform properties
	if properties, ok := params["properties"].(map[string]any); ok {
		transformedProps, err := transformProperties(properties)
		if err != nil {
			return nil, err
		}
		result["properties"] = transformedProps
	}

	// Handle required fields - validate parameter names
	if required, ok := params["required"].([]any); ok {
		var validRequired []string
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				if isValidParamName(reqStr) {
					validRequired = append(validRequired, reqStr)
				} else {
					// Transform invalid parameter names
					transformed := transformParamName(reqStr)
					if transformed != reqStr && isValidParamName(transformed) {
						validRequired = append(validRequired, transformed)
						// Update properties key if it was transformed
						if props, ok := result["properties"].(map[string]any); ok {
							if originalProp, exists := props[reqStr]; exists {
								delete(props, reqStr)
								props[transformed] = originalProp
							}
						}
					}
				}
			}
		}
		if len(validRequired) > 0 {
			result["required"] = validRequired
		}
	}

	// Set additionalProperties to false to be more restrictive
	result["additionalProperties"] = false

	return result, nil
}

// transformProperties transforms parameter properties, validating names and simplifying complex schemas
func transformProperties(properties map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	for propName, propSchema := range properties {
		// Validate and transform property name
		validName := propName
		if !isValidParamName(propName) {
			validName = transformParamName(propName)
			if !isValidParamName(validName) {
				// Skip properties with invalid names that can't be transformed
				continue
			}
		}

		// Transform property schema
		transformedSchema, err := transformPropertySchema(propSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to transform property '%s': %v", propName, err)
		}

		result[validName] = transformedSchema
	}

	return result, nil
}

// transformPropertySchema transforms individual property schemas to simpler formats
func transformPropertySchema(schema any) (map[string]any, error) {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		// If it's not a map, convert to simple string type
		return map[string]any{"type": "string"}, nil
	}

	result := make(map[string]any)

	// Handle anyOf, oneOf, allOf by simplifying to string type with description
	if _, ok := schemaMap["anyOf"]; ok {
		log.Printf("Simplifying anyOf schema to string type for JetBrains compatibility")
		result["type"] = "string"
		if desc, hasDesc := schemaMap["description"]; hasDesc {
			result["description"] = desc
		} else {
			result["description"] = "Complex type (anyOf) simplified to string"
		}
		return result, nil
	}

	if _, ok := schemaMap["oneOf"]; ok {
		log.Printf("Simplifying oneOf schema to string type for JetBrains compatibility")
		result["type"] = "string"
		if desc, hasDesc := schemaMap["description"]; hasDesc {
			result["description"] = desc
		} else {
			result["description"] = "Complex type (oneOf) simplified to string"
		}
		return result, nil
	}

	if _, ok := schemaMap["allOf"]; ok {
		log.Printf("Simplifying allOf schema to string type for JetBrains compatibility")
		result["type"] = "string"
		if desc, hasDesc := schemaMap["description"]; hasDesc {
			result["description"] = desc
		} else {
			result["description"] = "Complex type (allOf) simplified to string"
		}
		return result, nil
	}

	// Handle type
	if schemaType, ok := schemaMap["type"]; ok {
		result["type"] = schemaType
	} else {
		result["type"] = "string" // Default to string
	}

	// Simplify complex nested objects
	if typeStr, ok := result["type"].(string); ok {
		switch typeStr {
		case "object":
			// Check if this is a simple object or complex nested one
			if properties, hasProps := schemaMap["properties"].(map[string]any); hasProps {
				// Count properties to decide if we should simplify
				propCount := len(properties)

				// If it has too many properties, simplify to string
				// For nested objects, flatten them instead of converting to string
				if propCount > 10 {
					result["type"] = "string"
					result["description"] = "Complex object with many properties - provide as JSON string"
				} else {
					// Keep as object but flatten nested objects
					result["type"] = "object"
					simpleProps := make(map[string]any)
					for propName, propSchema := range properties {
						// Ensure property name is valid
						validName := propName
						if !isValidParamName(propName) {
							validName = transformParamName(propName)
						}
						if isValidParamName(validName) {
							// Check if this is a nested object and flatten it
							if propMap, ok := propSchema.(map[string]any); ok {
								if propType, ok := propMap["type"].(string); ok && propType == "object" {
									// Flatten nested object to string for JetBrains compatibility
									simpleProps[validName] = map[string]any{
										"type":        "string",
										"description": fmt.Sprintf("Nested object for %s - provide as JSON string", validName),
									}
								} else {
									simplified, _ := transformPropertySchema(propSchema)
									simpleProps[validName] = simplified
								}
							} else {
								simplified, _ := transformPropertySchema(propSchema)
								simpleProps[validName] = simplified
							}
						}
					}
					result["properties"] = simpleProps

					// Handle required fields for nested objects
					if req, hasReq := schemaMap["required"].([]any); hasReq {
						var validReq []string
						for _, r := range req {
							if rStr, ok := r.(string); ok {
								validName := rStr
								if !isValidParamName(rStr) {
									validName = transformParamName(rStr)
								}
								if isValidParamName(validName) {
									validReq = append(validReq, validName)
								}
							}
						}
						if len(validReq) > 0 {
							result["required"] = validReq
						}
					}

					result["additionalProperties"] = false
				}
			} else {
				// Object without properties definition - convert to string
				result["type"] = "string"
				result["description"] = "Object without properties - provide as JSON string"
			}

		case "array":
			// Keep array but simplify items
			result["type"] = "array"
			if items, ok := schemaMap["items"]; ok {
				if itemsMap, ok := items.(map[string]any); ok {
					if itemType, ok := itemsMap["type"]; ok {
						result["items"] = map[string]any{"type": itemType}
					} else {
						result["items"] = map[string]any{"type": "string"}
					}
				} else {
					result["items"] = map[string]any{"type": "string"}
				}
			} else {
				result["items"] = map[string]any{"type": "string"}
			}
		}
	}

	// Copy simple properties
	for key, value := range schemaMap {
		switch key {
		case "description", "enum", "pattern", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems":
			result[key] = value
		case "format":
			// Only copy supported formats
			if formatStr, ok := value.(string); ok {
				switch formatStr {
				case "email", "uri", "date", "date-time":
					result[key] = value
				}
			}
		}
	}

	// Handle enum values
	if enum, ok := schemaMap["enum"]; ok {
		result["enum"] = enum
	}

	return result, nil
}

// isValidParamName checks if a parameter name matches JetBrains API requirements
func isValidParamName(name string) bool {
	return len(name) <= MaxParamNameLength && paramNameRegex.MatchString(name)
}

// transformParamName transforms invalid parameter names to valid ones
func transformParamName(name string) string {
	// Remove invalid characters and truncate
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			if builder.Len() < MaxParamNameLength {
				builder.WriteRune(r)
			}
		}
	}

	result := builder.String()
	if result == "" {
		result = "param"
	}

	// Ensure it doesn't exceed length limit
	if len(result) > MaxParamNameLength {
		result = result[:MaxParamNameLength]
	}

	return result
}

// validateToolCallResponse validates that a tool call response is properly formatted
func validateToolCallResponse(toolCall ToolCall) error {
	if toolCall.Function.Name == "" {
		return fmt.Errorf("tool call function name is empty")
	}

	if !isValidParamName(toolCall.Function.Name) {
		return fmt.Errorf("tool call function name '%s' is invalid", toolCall.Function.Name)
	}

	// Validate arguments JSON
	if toolCall.Function.Arguments != "" {
		var args map[string]any
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return fmt.Errorf("tool call arguments are not valid JSON: %v", err)
		}
	}

	return nil
}
