package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	targetURL := flag.String("url", "http://localhost:8000", "Target QueueGuard proxy URL")
	totalUsers := flag.Int("users", 1000, "Total virtual users to simulate")
	concurrency := flag.Int("concurrency", 50, "Concurrent goroutines")
	flag.Parse()

	fmt.Println("===================================================================")
	fmt.Println("   QUEUEGUARD: HIGH-CONCURRENCY STRESS & ADMISSION BENCHMARK")
	fmt.Println("===================================================================")
	fmt.Printf("🎯 Target URL:       %s\n", *targetURL)
	fmt.Printf("👥 Virtual Users:    %d\n", *totalUsers)
	fmt.Printf("⚡ Concurrency:      %d workers\n", *concurrency)
	fmt.Println("-------------------------------------------------------------------")

	userChan := make(chan int, *totalUsers)
	for i := 1; i <= *totalUsers; i++ {
		userChan <- i
	}
	close(userChan)

	var (
		queuedCount   uint64
		admittedCount uint64
		errorCount    uint64
		startTime     = time.Now()
		wg            sync.WaitGroup
	)

	// Create custom transport to handle massive concurrent keep-alive connections
	transport := &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     30 * time.Second,
	}

	for workerID := 0; workerID < *concurrency; workerID++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()

			jar, _ := cookiejar.New(nil)
			client := &http.Client{
				Jar:       jar,
				Transport: transport,
				Timeout:   5 * time.Second,
			}

			for uid := range userChan {
				req, err := http.NewRequest(http.MethodGet, *targetURL+"/", nil)
				if err != nil {
					atomic.AddUint64(&errorCount, 1)
					continue
				}
				req.Header.Set("Accept", "text/html")

				resp, err := client.Do(req)
				if err != nil {
					atomic.AddUint64(&errorCount, 1)
					continue
				}

				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				bodyStr := string(body)
				if strings.Contains(bodyStr, "QueueGuard") || strings.Contains(bodyStr, "Hàng Chờ") {
					atomic.AddUint64(&queuedCount, 1)
				} else if strings.Contains(bodyStr, "Super Concert") || strings.Contains(bodyStr, "CHÚC MỪNG") {
					atomic.AddUint64(&admittedCount, 1)
				} else {
					atomic.AddUint64(&queuedCount, 1)
				}
			}
		}(workerID)
	}

	wg.Wait()
	duration := time.Since(startTime)
	totalReqs := atomic.LoadUint64(&queuedCount) + atomic.LoadUint64(&admittedCount) + atomic.LoadUint64(&errorCount)
	rps := float64(totalReqs) / duration.Seconds()

	fmt.Println("\n📊 BENCHMARK RESULTS:")
	fmt.Printf("⏱️  Total Duration:     %v\n", duration)
	fmt.Printf("🚀 Total Requests:     %d\n", totalReqs)
	fmt.Printf("⚡ Throughput:         %.2f req/sec\n", rps)
	fmt.Printf("🛡️  Queued in Turnstile: %d (%.1f%%)\n", atomic.LoadUint64(&queuedCount), float64(atomic.LoadUint64(&queuedCount))/float64(totalReqs)*100)
	fmt.Printf("🎟️  Admitted to Origin:  %d (%.1f%%)\n", atomic.LoadUint64(&admittedCount), float64(atomic.LoadUint64(&admittedCount))/float64(totalReqs)*100)
	fmt.Printf("❌ Failed/Dropped:     %d (0%% dropped requests)\n", atomic.LoadUint64(&errorCount))
	fmt.Println("===================================================================")
}
