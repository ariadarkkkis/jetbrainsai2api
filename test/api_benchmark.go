package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

func main() {
	fmt.Println("🚀 开始API性能测试...")
	
	// 测试配置
	concurrency := 3
	duration := 10 * time.Second
	
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 创建测试请求
	testRequest := map[string]interface{}{
		"model": "o4-mini",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": "Hello, please respond briefly with 'Test OK'",
			},
		},
		"max_tokens":   20,
		"temperature": 0.7,
	}

	requestBody, _ := json.Marshal(testRequest)
	url := "http://localhost:7860/v1/chat/completions"

	results := make(chan time.Duration, concurrency*50)
	errors := make(chan error, concurrency*50)
	var wg sync.WaitGroup

	fmt.Printf("🔢 并发数: %d\n", concurrency)
	fmt.Printf("⏱️  持续时间: %v\n", duration)

	// 启动结果收集器
	var totalDuration time.Duration
	var successfulRequests int
	var failedRequests int
	var minResponseTime, maxResponseTime time.Duration
	
	go func() {
		for duration := range results {
			totalDuration += duration
			successfulRequests++
			
			if minResponseTime == 0 || duration < minResponseTime {
				minResponseTime = duration
			}
			if duration > maxResponseTime {
				maxResponseTime = duration
			}
		}
	}()
	
	go func() {
		for range errors {
			failedRequests++
		}
	}()

	// 启动并发worker
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			start := time.Now()
			for time.Since(start) < duration {
				reqStart := time.Now()
				
				// 创建HTTP请求
				req, err := http.NewRequest("POST", url, bytes.NewReader(requestBody))
				if err != nil {
					errors <- err
					continue
				}
				
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer testofli")
				
				// 发送请求
				resp, err := client.Do(req)
				if err != nil {
					errors <- err
					continue
				}
				
				// 读取响应
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					errors <- err
					continue
				}
				
				if resp.StatusCode == http.StatusOK {
					results <- time.Since(reqStart)
				} else {
					errors <- fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
				}
			}
		}()
	}

	// 等待所有worker完成
	wg.Wait()
	close(results)
	close(errors)
	
	// 等待结果收集完成
	time.Sleep(100 * time.Millisecond)

	// 计算QPS
	qps := float64(successfulRequests) / duration.Seconds()

	avgResponseTime := time.Duration(0)
	if successfulRequests > 0 {
		avgResponseTime = totalDuration / time.Duration(successfulRequests)
	}

	// 输出结果
	fmt.Printf("\n🎯 性能测试结果:\n")
	fmt.Printf("✅ 总请求数: %d\n", successfulRequests+failedRequests)
	fmt.Printf("✅ 成功请求: %d\n", successfulRequests)
	fmt.Printf("❌ 失败请求: %d\n", failedRequests)
	fmt.Printf("📈 QPS: %.2f\n", qps)
	fmt.Printf("⏱️  平均响应时间: %v\n", avgResponseTime)
	fmt.Printf("⚡ 最快响应时间: %v\n", minResponseTime)
	fmt.Printf("🐌 最慢响应时间: %v\n", maxResponseTime)

	// 计算成功率
	if successfulRequests+failedRequests > 0 {
		successRate := float64(successfulRequests) / float64(successfulRequests+failedRequests) * 100
		fmt.Printf("🎯 成功率: %.2f%%\n", successRate)
	}

	fmt.Println("\n🎉 性能测试完成!")
}