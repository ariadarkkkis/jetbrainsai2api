package main

import (
	json "github.com/json-iterator/go"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultRequestTimeout = 30 * time.Second
	QuotaCacheTime        = time.Hour
	JWTRefreshTime        = 12 * time.Hour
)

// Global variables
var (
	validClientKeys        = make(map[string]bool)
	jetbrainsAccounts      []JetbrainsAccount
	currentAccountIndex    int
	accountRotationLock    sync.Mutex
	modelsData             ModelsData
	modelsConfig           ModelsConfig
	anthropicModelMappings map[string]string
	httpClient             *http.Client
	requestStats           RequestStats
	statsMutex             sync.Mutex
)



func main() {
	// 加载.env文件
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize HTTP client
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient = &http.Client{
		Transport: transport,
		Timeout:   DefaultRequestTimeout,
	}

	// Load configuration
	modelsData = loadModels()
	// Load the full config for internal model lookup
	data, err := os.ReadFile("models.json")
	if err == nil {
		json.Unmarshal(data, &modelsConfig)
	}
	loadClientAPIKeys()
	loadJetbrainsAccounts()

	// Start pprof server
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	r := setupRoutes()

	log.Println("Starting JetBrains AI OpenAI Compatible API server...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "7860"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}