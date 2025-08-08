package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

func RunPerformanceTests() {
	// 解析命令行参数
	var (
		mode        = flag.String("mode", "test", "运行模式: test, benchmark, monitor")
		duration    = flag.Duration("duration", 30*time.Second, "测试持续时间")
		concurrency = flag.Int("concurrency", 10, "并发请求数")
		profile     = flag.Bool("profile", false, "是否启用性能分析")
	)
	flag.Parse()

	switch *mode {
	case "test":
		performanceTestSuite()
	case "benchmark":
		runBenchmarkTests(*duration, *concurrency, *profile)
	case "monitor":
		runMonitoring()
	default:
		fmt.Println("未知模式，支持: test, benchmark, monitor")
		os.Exit(1)
	}
}

// runBenchmarkTests 运行基准测试
func runBenchmarkTests(duration time.Duration, concurrency int, profile bool) {
	fmt.Printf("🚀 开始基准测试...\n")
	fmt.Printf("⏱️  持续时间: %v\n", duration)
	fmt.Printf("🔢 并发数: %d\n", concurrency)
	fmt.Printf("📊 性能分析: %v\n", profile)

	if profile {
		// 启动CPU性能分析
		f, err := os.Create("cpu.prof")
		if err != nil {
			log.Fatal("无法创建CPU分析文件: ", err)
		}
		defer f.Close()
		
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("无法启动CPU分析: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	// 运行并发测试
	start := time.Now()
	requests := make(chan int, concurrency*10)
	results := make(chan time.Duration, concurrency*10)

	// 启动worker
	for i := 0; i < concurrency; i++ {
		go func() {
			for range requests {
				reqStart := time.Now()
				
				// 模拟计算密集型操作
				var result int64
				for i := 0; i < 1000000; i++ {
					result += int64(i * i)
				}
				
				results <- time.Since(reqStart)
			}
		}()
	}

	// 发送请求
	go func() {
		reqCount := 0
		for time.Since(start) < duration {
			reqCount++
			requests <- reqCount
		}
		close(requests)
	}()

	// 收集结果
	var totalDuration time.Duration
	var successCount int
	var minDuration, maxDuration time.Duration

	for result := range results {
		totalDuration += result
		successCount++
		
		if minDuration == 0 || result < minDuration {
			minDuration = result
		}
		if result > maxDuration {
			maxDuration = result
		}
		
		// 检查是否结束
		if time.Since(start) >= duration && len(results) == 0 {
			break
		}
	}

	// 输出结果
	actualDuration := time.Since(start)
	fmt.Printf("\n🎯 基准测试结果:\n")
	fmt.Printf("✅ 总请求数: %d\n", successCount)
	fmt.Printf("⏱️  实际耗时: %v\n", actualDuration)
	fmt.Printf("📈 QPS: %.2f\n", float64(successCount)/actualDuration.Seconds())
	fmt.Printf("⏱️  平均响应时间: %v\n", totalDuration/time.Duration(successCount))
	fmt.Printf("⚡ 最快响应时间: %v\n", minDuration)
	fmt.Printf("🐌 最慢响应时间: %v\n", maxDuration)

	// 内存统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("💾 内存使用: %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("🔄 GC次数: %d\n", m.NumGC)

	if profile {
		// 生成内存分析
		f, err := os.Create("mem.prof")
		if err != nil {
			log.Fatal("无法创建内存分析文件: ", err)
		}
		defer f.Close()
		
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("无法写入内存分析: ", err)
		}
		
		fmt.Printf("📊 性能分析文件已生成: cpu.prof, mem.prof\n")
	}
}

// runMonitoring 运行监控模式
func runMonitoring() {
	fmt.Println("📊 启动性能监控模式...")
	
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			fmt.Printf("\n=== 实时性能监控 ===\n")
			fmt.Printf("监控时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
			
			// 内存统计
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Printf("内存使用: %d MB\n", m.Alloc/1024/1024)
			fmt.Printf("协程数量: %d\n", runtime.NumGoroutine())
			fmt.Printf("GC次数: %d\n", m.NumGC)
			
			fmt.Printf("==================\n")
		}
	}
}

// performanceTestSuite 运行性能测试套件
func performanceTestSuite() {
	fmt.Println("🚀 开始性能基准测试...")
	
	// 测试计算性能
	fmt.Println("🔧 测试计算性能...")
	computeStart := time.Now()
	
	for i := 0; i < 1000; i++ {
		var result int64
		for j := 0; j < 1000000; j++ {
			result += int64(j * j)
		}
	}
	
	computeDuration := time.Since(computeStart)
	fmt.Printf("✅ 计算性能测试完成: 1000次计算耗时 %v\n", computeDuration)
	fmt.Printf("📈 平均每次计算耗时: %v\n", computeDuration/1000)
	
	fmt.Println("🎉 性能基准测试完成!")
}

func main() {
	RunPerformanceTests()
}