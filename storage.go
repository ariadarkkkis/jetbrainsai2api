package main

import (
	"context"
	"log"
	"os"

	"github.com/bytedance/sonic"
	"github.com/redis/go-redis/v9"
)

const (
	statsRedisKey = "jetbrainsai2api:stats"
)

// StorageInterface defines the interface for persistent storage
type StorageInterface interface {
	SaveStats(stats *RequestStats) error
	LoadStats() (*RequestStats, error)
}

// FileStorage implements persistence using JSON files
type FileStorage struct{}

func (fs *FileStorage) SaveStats(stats *RequestStats) error {
	data, err := sonic.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statsFilePath, data, 0644)
}

func (fs *FileStorage) LoadStats() (*RequestStats, error) {
	data, err := os.ReadFile(statsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty stats if file doesn't exist
			return &RequestStats{
				RequestHistory: []RequestRecord{},
			}, nil
		}
		return nil, err
	}

	var stats RequestStats
	if err := sonic.Unmarshal(data, &stats); err != nil {
		return nil, err
	}

	// Ensure history is not nil
	if stats.RequestHistory == nil {
		stats.RequestHistory = []RequestRecord{}
	}

	return &stats, nil
}

// RedisStorage implements persistence using Redis
type RedisStorage struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisStorage(redisURL string) (*RedisStorage, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)
	ctx := context.Background()

	// Test connection
	_, err = client.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}

	log.Printf("Successfully connected to Redis")
	return &RedisStorage{
		client: client,
		ctx:    ctx,
	}, nil
}

func (rs *RedisStorage) SaveStats(stats *RequestStats) error {
	data, err := marshalJSON(stats)
	if err != nil {
		return err
	}

	return rs.client.Set(rs.ctx, statsRedisKey, data, 0).Err()
}

func (rs *RedisStorage) LoadStats() (*RequestStats, error) {
	val, err := rs.client.Get(rs.ctx, statsRedisKey).Result()
	if err != nil {
		if err == redis.Nil {
			// Return empty stats if key doesn't exist
			return &RequestStats{
				RequestHistory: []RequestRecord{},
			}, nil
		}
		return nil, err
	}

	var stats RequestStats
	if err := sonic.Unmarshal([]byte(val), &stats); err != nil {
		return nil, err
	}

	// Ensure history is not nil
	if stats.RequestHistory == nil {
		stats.RequestHistory = []RequestRecord{}
	}

	return &stats, nil
}

func (rs *RedisStorage) Close() error {
	return rs.client.Close()
}

// Global storage instance
var storage StorageInterface

// initStorage initializes the storage based on environment configuration
func initStorage() error {
	redisURL := os.Getenv("REDIS_URL")

	if redisURL != "" {
		// Use Redis storage
		redisStorage, err := NewRedisStorage(redisURL)
		if err != nil {
			log.Printf("Failed to initialize Redis storage: %v, falling back to file storage", err)
			storage = &FileStorage{}
		} else {
			storage = redisStorage
			log.Printf("Using Redis storage")
		}
	} else {
		// Use file storage
		storage = &FileStorage{}
		log.Printf("Using file storage")
	}

	return nil
}

// saveStatsWithStorage saves stats using the configured storage
func saveStatsWithStorage() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	if err := storage.SaveStats(&requestStats); err != nil {
		log.Printf("Error saving stats: %v", err)
	}
}

// loadStatsWithStorage loads stats using the configured storage
func loadStatsWithStorage() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	stats, err := storage.LoadStats()
	if err != nil {
		log.Printf("Error loading stats: %v", err)
		// Initialize with empty stats if loading fails
		requestStats = RequestStats{
			RequestHistory: []RequestRecord{},
		}
		return
	}

	requestStats = *stats
	log.Printf("Successfully loaded %d request records", len(requestStats.RequestHistory))
}
