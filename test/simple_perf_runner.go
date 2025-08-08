package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
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
	
	// 内存统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("💾 内存使用: %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("🔄 GC次数: %d\n", m.NumGC)
	fmt.Printf("🧵 协程数量: %d\n", runtime.NumGoroutine())
	
	fmt.Println("🎉 性能基准测试完成!")
}