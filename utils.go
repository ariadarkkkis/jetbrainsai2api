package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

// extractTextContent extracts text from a message's content field.
func extractTextContent(content any) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []any:
		var textParts []string
		for _, item := range v {
			if itemMap, ok := item.(map[string]any); ok {
				if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
					if text, ok := itemMap["text"].(string); ok {
						textParts = append(textParts, text)
					}
				}
			}
		}
		return strings.Join(textParts, " ")
	}
	return ""
}

// recordFailureWithTimer records a failed request with elapsed time
func recordFailureWithTimer(startTime time.Time, model, account string) {
	recordRequest(false, time.Since(startTime).Milliseconds(), model, account)
}

// parseEnvList parses comma-separated environment variable into trimmed slice
func parseEnvList(envVar string) []string {
	if envVar == "" {
		return nil
	}
	parts := strings.Split(envVar, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// marshalJSON wraps sonic.Marshal for consistent error handling
func marshalJSON(v interface{}) ([]byte, error) {
	return sonic.Marshal(v)
}

// respondWithError sends a JSON error response
func respondWithError(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{"error": message})
}

// getEnvWithDefault gets environment variable with default value
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// createJetbrainsRequest creates an HTTP request for JetBrains API with standard headers
func createJetbrainsRequest(method, url string, payload interface{}, authorization string) (*http.Request, error) {
	var body io.Reader

	if payload != nil {
		payloadBytes, err := marshalJSON(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(payloadBytes)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("authorization", "Bearer "+authorization)
	}

	return req, nil
}
