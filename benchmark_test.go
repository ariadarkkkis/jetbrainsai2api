package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// BenchmarkSuite 性能基准测试套件
type BenchmarkSuite struct {
	router *gin.Engine
	server *httptest.Server
}

// NewBenchmarkSuite 创建新的基准测试套件
func NewBenchmarkSuite() *BenchmarkSuite {
	gin.SetMode(gin.ReleaseMode)
	
	// 初始化必要的数据
	validClientKeys["test-key"] = true
	modelsData = loadModels()
	
	suite := &BenchmarkSuite{
		router: setupRoutes(),
	}
	
	suite.server = httptest.NewServer(suite.router)
	return suite
}

// Close 关闭测试服务器
func (suite *BenchmarkSuite) Close() {
	suite.server.Close()
}

// BenchmarkHTTPConnections 测试HTTP连接性能
func BenchmarkHTTPConnections(b *testing.B) {
	suite := NewBenchmarkSuite()
	defer suite.Close()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", suite.server.URL+"/v1/models", nil)
			req.Header.Set("Authorization", "Bearer test-key")
			
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
		}
	})
}

// BenchmarkToolValidation 测试工具验证性能
func BenchmarkToolValidation(b *testing.B) {
	// 创建复杂的工具定义
	complexTool := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "complex_test_function",
			Description: "A complex tool for testing validation performance",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nested_param": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"deep_nested": map[string]any{
								"type": "string",
								"anyOf": []any{
									map[string]any{"type": "string"},
									map[string]any{"type": "number"},
									map[string]any{"type": "null"},
								},
							},
						},
					},
				},
			},
		},
	}

	tools := []Tool{complexTool}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validateAndTransformTools(tools)
		if err != nil {
			b.Fatalf("Tool validation failed: %v", err)
		}
	}
}

// BenchmarkCachePerformance 测试缓存性能
func BenchmarkCachePerformance(b *testing.B) {
	cache := NewCache()
	testKey := "benchmark_key"
	testValue := "benchmark_value"

	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Set(fmt.Sprintf("%s_%d", testKey, i), testValue, time.Hour)
		}
	})

	b.Run("Get", func(b *testing.B) {
		// 预填充缓存
		for i := 0; i < 1000; i++ {
			cache.Set(fmt.Sprintf("%s_%d", testKey, i), testValue, time.Hour)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get(fmt.Sprintf("%s_%d", testKey, i%1000))
		}
	})
}

// BenchmarkConcurrentRequests 测试并发请求性能
func BenchmarkConcurrentRequests(b *testing.B) {
	suite := NewBenchmarkSuite()
	defer suite.Close()

	// 创建测试请求
	request := ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello, world!"},
		},
		Stream: false,
	}

	requestBody, _ := json.Marshal(request)

	b.RunParallel(func(pb *testing.PB) {
		client := &http.Client{Timeout: 30 * time.Second}
		
		for pb.Next() {
			req, _ := http.NewRequest("POST", suite.server.URL+"/v1/chat/completions", bytes.NewBuffer(requestBody))
			req.Header.Set("Authorization", "Bearer test-key")
			req.Header.Set("Content-Type", "application/json")
			
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
		}
	})
}

// BenchmarkMemoryUsage 测试内存使用情况
func BenchmarkMemoryUsage(b *testing.B) {
	suite := NewBenchmarkSuite()
	defer suite.Close()

	// 创建大量复杂的工具定义
	var tools []Tool
	for i := 0; i < 100; i++ {
		tool := Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        fmt.Sprintf("tool_%d", i),
				Description: fmt.Sprintf("Tool number %d for memory testing", i),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						fmt.Sprintf("param_%d", i): map[string]any{
							"type": "string",
							"description": fmt.Sprintf("Parameter %d for tool %d", i, i),
						},
					},
				},
			},
		}
		tools = append(tools, tool)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validateAndTransformTools(tools)
		if err != nil {
			b.Fatalf("Memory benchmark failed: %v", err)
		}
	}
}

// BenchmarkStreamingResponse 测试流式响应性能
func BenchmarkStreamingResponse(b *testing.B) {
	suite := NewBenchmarkSuite()
	defer suite.Close()

	request := ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []ChatMessage{
			{Role: "user", Content: "Generate a long response"},
		},
		Stream: true,
	}

	requestBody, _ := json.Marshal(request)

	b.RunParallel(func(pb *testing.PB) {
		client := &http.Client{Timeout: 30 * time.Second}
		
		for pb.Next() {
			req, _ := http.NewRequest("POST", suite.server.URL+"/v1/chat/completions", bytes.NewBuffer(requestBody))
			req.Header.Set("Authorization", "Bearer test-key")
			req.Header.Set("Content-Type", "application/json")
			
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			
			// 读取流式响应
			buf := make([]byte, 1024)
			for {
				_, err := resp.Body.Read(buf)
				if err != nil {
					break
				}
			}
			resp.Body.Close()
		}
	})
}

// runPerformanceTests 运行完整的性能测试套件
func runPerformanceTests() {
	fmt.Println("🚀 开始性能基准测试...")
	
	// 创建测试套件
	suite := NewBenchmarkSuite()
	defer suite.Close()

	// 运行各种性能测试
	fmt.Println("📊 运行HTTP连接性能测试...")
	start := time.Now()
	
	// 模拟并发请求
	var wg sync.WaitGroup
	concurrentRequests := 100
	
	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			client := &http.Client{Timeout: 30 * time.Second}
			req, _ := http.NewRequest("GET", suite.server.URL+"/v1/models", nil)
			req.Header.Set("Authorization", "Bearer test-key")
			
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	fmt.Printf("✅ HTTP连接性能测试完成: %d 并发请求耗时 %v\n", concurrentRequests, duration)
	fmt.Printf("📈 平均每个请求耗时: %v\n", duration/time.Duration(concurrentRequests))
	
	// 测试工具验证性能
	fmt.Println("🔧 测试工具验证性能...")
	toolStart := time.Now()
	
	complexTool := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "performance_test_function",
			Description: "Complex tool for performance testing",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"complex_param": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"nested_field": map[string]any{
								"type": "string",
								"anyOf": []any{
									map[string]any{"type": "string"},
									map[string]any{"type": "number"},
								},
							},
						},
					},
				},
			},
		},
	}
	
	for i := 0; i < 1000; i++ {
		validateAndTransformTools([]Tool{complexTool})
	}
	
	toolDuration := time.Since(toolStart)
	fmt.Printf("✅ 工具验证性能测试完成: 1000次验证耗时 %v\n", toolDuration)
	fmt.Printf("📈 平均每次验证耗时: %v\n", toolDuration/1000)
	
	fmt.Println("🎉 性能基准测试完成!")
}