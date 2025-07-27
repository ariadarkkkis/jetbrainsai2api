package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// anthropicMessages 处理Anthropic兼容的messages请求
func anthropicMessages(c *gin.Context) {
	startTime := time.Now()
	var request AnthropicMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), "", "")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Map Anthropic model to OpenAI model if needed
	originalModel := request.Model
	if mappedModel, exists := anthropicModelMappings[request.Model]; exists {
		request.Model = mappedModel
		log.Printf("Mapped Anthropic model %s to %s", originalModel, request.Model)
	}

	// Convert Anthropic request to OpenAI format
	_, err := convertAnthropicToOpenAI(request)
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to convert request: %v", err)})
		return
	}

	// Process as OpenAI request
	_, err = getNextJetbrainsAccount()
	if err != nil {
		recordRequest(false, time.Since(startTime).Milliseconds(), request.Model, "")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}

	// Continue with the same logic as chatCompletions but return Anthropic format
	// For now, return a simple error indicating this endpoint needs implementation
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Anthropic messages endpoint not fully implemented yet"})
}

