package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// setupTestApp initializes the gin router for testing.
func setupTestApp() *gin.Engine {
	gin.SetMode(gin.TestMode)

	// Initialize HTTP client for testing
	httpClient = &http.Client{Timeout: 30 * time.Second}

	// Load config for test
	modelsData = loadModels()
	loadClientAPIKeys()
	loadJetbrainsAccounts()
	r := setupRoutes()
	return r
}

// TestChatCompletions_Success is a basic integration test for the chat completions endpoint.
func TestChatCompletions_Success(t *testing.T) {
	// Setup
	router := setupTestApp()

	// Mock request body
	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "hello"}]}`

	// Create a test recorder and request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(body))

	// Get a valid API key from environment variables
	apiKey := os.Getenv("CLIENT_API_KEYS")
	if apiKey == "" {
		t.Fatal("CLIENT_API_KEYS environment variable not set for testing. Please set it to a valid key.")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Execute the request
	router.ServeHTTP(w, req)

	// Assert the response
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, but got %d. Response: %s", http.StatusOK, w.Code, w.Body.String())
	}

	t.Logf("Successfully tested chat completions endpoint with status: %d", w.Code)
}
