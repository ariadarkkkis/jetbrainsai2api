package main

import (
	"context"
	"expvar"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// PerformanceMetrics 性能指标收集器
type PerformanceMetrics struct {
	mu sync.RWMutex

	// HTTP相关指标
	httpRequests    int64
	httpErrors      int64
	avgResponseTime float64

	// 缓存相关指标
	cacheHits    int64
	cacheMisses  int64
	cacheHitRate float64

	// 工具验证相关指标
	toolValidations    int64
	toolValidationTime float64

	// 账户管理相关指标
	accountPoolWait   int64
	accountPoolErrors int64

	// 系统资源指标
	memoryUsage    uint64
	goroutineCount int

	// 时间窗口统计
	windowStartTime time.Time
	windowRequests  int64
}

var (
	metrics = &PerformanceMetrics{
		windowStartTime: time.Now(),
	}

	// expvar 统计变量
	httpRequestsVar    = expvar.NewInt("http_requests_total")
	httpErrorsVar      = expvar.NewInt("http_errors_total")
	cacheHitsVar       = expvar.NewInt("cache_hits_total")
	cacheMissesVar     = expvar.NewInt("cache_misses_total")
	toolValidationsVar = expvar.NewInt("tool_validations_total")
	avgResponseTimeVar = expvar.NewFloat("avg_response_time_ms")

	// 监控控制
	monitorCtx    context.Context
	monitorCancel context.CancelFunc
)

// RecordHTTPRequest 记录HTTP请求
func RecordHTTPRequest(duration time.Duration) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.httpRequests++
	metrics.windowRequests++

	// 计算平均响应时间
	if metrics.avgResponseTime == 0 {
		metrics.avgResponseTime = float64(duration.Milliseconds())
	} else {
		metrics.avgResponseTime = (metrics.avgResponseTime*0.9 + float64(duration.Milliseconds())*0.1)
	}

	// 更新expvar
	httpRequestsVar.Add(1)
	avgResponseTimeVar.Set(metrics.avgResponseTime)
}

// RecordHTTPError 记录HTTP错误
func RecordHTTPError() {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.httpErrors++
	httpErrorsVar.Add(1)
}

// RecordCacheHit 记录缓存命中
func RecordCacheHit() {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.cacheHits++

	// 计算缓存命中率
	total := metrics.cacheHits + metrics.cacheMisses
	if total > 0 {
		metrics.cacheHitRate = float64(metrics.cacheHits) / float64(total)
	}

	cacheHitsVar.Add(1)
}

// RecordCacheMiss 记录缓存未命中
func RecordCacheMiss() {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.cacheMisses++

	// 计算缓存命中率
	total := metrics.cacheHits + metrics.cacheMisses
	if total > 0 {
		metrics.cacheHitRate = float64(metrics.cacheHits) / float64(total)
	}

	cacheMissesVar.Add(1)
}

// RecordToolValidation 记录工具验证
func RecordToolValidation(duration time.Duration) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.toolValidations++

	if metrics.toolValidationTime == 0 {
		metrics.toolValidationTime = float64(duration.Milliseconds())
	} else {
		metrics.toolValidationTime = (metrics.toolValidationTime*0.9 + float64(duration.Milliseconds())*0.1)
	}

	toolValidationsVar.Add(1)
}

// RecordAccountPoolWait 记录账户池等待
func RecordAccountPoolWait(duration time.Duration) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.accountPoolWait++
}

// RecordAccountPoolError 记录账户池错误
func RecordAccountPoolError() {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.accountPoolErrors++
}

// UpdateSystemMetrics 更新系统资源指标
func UpdateSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.memoryUsage = m.Alloc
	metrics.goroutineCount = runtime.NumGoroutine()
}

// ResetWindow 重置时间窗口统计
func ResetWindow() {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.windowStartTime = time.Now()
	metrics.windowRequests = 0
}

// GetMetricsString 获取性能指标字符串
func GetMetricsString() string {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	// 安全计算错误率，避免除零错误
	errorRate := 0.0
	if metrics.httpRequests > 0 {
		errorRate = float64(metrics.httpErrors) / float64(metrics.httpRequests) * 100
	}

	return fmt.Sprintf(`=== Performance Statistics ===
HTTP Requests:
- Total Requests: %d
- Errors: %d
- Error Rate: %.2f%%
- Average Response Time: %.2fms

Cache Performance:
- Cache Hits: %d
- Cache Misses: %d
- Hit Rate: %.2f%%

Tool Verification:
- Verifications: %d
- Average Verification Time: %.2fms

Account Management:
- Account Pool Waits: %d
- Account Pool Errors: %d

System Resources:
- Memory Usage: %d MB
- Number of Coroutines: %d

Current Window:
- Window Start Time: %s
- Window Requests: %d
`,
		metrics.httpRequests,
		metrics.httpErrors,
		errorRate,
		metrics.avgResponseTime,

		metrics.cacheHits,
		metrics.cacheMisses,
		metrics.cacheHitRate*100,

		metrics.toolValidations,
		metrics.toolValidationTime,

		metrics.accountPoolWait,
		metrics.accountPoolErrors,

		metrics.memoryUsage/1024/1024,
		metrics.goroutineCount,

		metrics.windowStartTime.Format("2006-01-02 15:04:05"),
		metrics.windowRequests,
	)
}

// GetQPS 获取当前QPS
func GetQPS() float64 {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	windowDuration := time.Since(metrics.windowStartTime).Seconds()
	if windowDuration == 0 {
		return 0
	}

	return float64(metrics.windowRequests) / windowDuration
}

// StartMetricsMonitor 启动性能监控
func StartMetricsMonitor() {
	// 创建带取消的 context，解决 goroutine 泄漏问题
	monitorCtx, monitorCancel = context.WithCancel(context.Background())

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				UpdateSystemMetrics()

				// 每5分钟重置窗口统计
				if time.Since(metrics.windowStartTime) > 5*time.Minute {
					ResetWindow()
				}

				// 在debug模式下输出性能指标
				Debug("=== Performance Monitoring Report ===")
				Debug("Current QPS: %.2f", GetQPS())
				Debug("%s", GetMetricsString())
				Debug("=====================")
			case <-monitorCtx.Done():
				// 收到停止信号，优雅退出监控 goroutine
				return
			}
		}
	}()
}

// StopMetricsMonitor 停止性能监控
func StopMetricsMonitor() {
	if monitorCancel != nil {
		monitorCancel()
	}
}

// 初始化性能监控
func init() {
	StartMetricsMonitor()
}
