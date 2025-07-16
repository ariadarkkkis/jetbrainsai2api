package main

import (
	json "github.com/json-iterator/go"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultRequestTimeout = 30 * time.Second
	QuotaCacheTime        = time.Hour
	JWTRefreshTime        = 12 * time.Hour
	StatsSaveInterval     = 1 * time.Minute
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
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Load statistics from file
	loadStats()

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
	data, err := os.ReadFile("models.json")
	if err == nil {
		json.Unmarshal(data, &modelsConfig)
	}
	loadClientAPIKeys()
	loadJetbrainsAccounts()

	// Set up periodic saving of statistics
	ticker := time.NewTicker(StatsSaveInterval)
	go func() {
		for range ticker.C {
			log.Println("Periodically saving statistics...")
			saveStats()
		}
	}()

	// Set up graceful shutdown
	setupGracefulShutdown()

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

func setupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutdown signal received, saving statistics before exiting...")
		saveStats()
		os.Exit(0)
	}()
}