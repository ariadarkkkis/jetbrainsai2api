package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestApp() *gin.Engine {
	gin.SetMode(gin.TestMode)
	// Load config for test
	modelsData = loadModels()
	loadClientAPIKeys()
	loadJetbrainsAccounts()
	r := setupRoutes()
	return r
}

func TestChatCompletions(t *testing.T) {
	// Setup
	router := setupTestApp()

	// Mock request
	body := `{`
		`"model": "gpt-4",`
		`"messages": [{"role": "user", "content": "hello"}]`
	`}`

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(body))
	// Set a valid API key for testing
	apiKey := os.Getenv("CLIENT_API_KEYS")
	if apiKey == "" {
		t.Fatal("CLIENT_API_KEYS environment variable not set for testing")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func BenchmarkChatCompletions(b *testing.B) {
	// Setup
	router := setupTestApp()
	apiKey := os.Getenv("CLIENT_API_KEYS")
	if apiKey == "" {
		b.Fatal("CLIENT_API_KEYS environment variable not set for testing")
	}

	// Mock request
	body := `{`
		`"model": "gpt-4",`
		`"messages": [{"role": "user", "content": "hello"}]`
	`}`

	// Run benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
	}
}
