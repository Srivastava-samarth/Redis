package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	totalRequests = 1000
	concurrency   = 100
	apiURL        = "http://localhost:8080/urls"
)

func main() {
	client := &http.Client{}

	var wg sync.WaitGroup
	var mu sync.Mutex

	successCount := 0
	errorCount := 0

	var totalLatency time.Duration
	minLatency := time.Duration(1<<63 - 1)
	maxLatency := time.Duration(0)

	start := time.Now()

	requests := make(chan int)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range requests {
				req, err := http.NewRequest(
					http.MethodPost,
					apiURL,
					strings.NewReader(
						fmt.Sprintf(
							`{"url":"https://example.com/%d"}`,
							i,
						),
					),
				)

				if err != nil {
					mu.Lock()
					errorCount++
					mu.Unlock()
					continue
				}

				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Client-ID", "api-loadtest")
				req.Header.Set(
					"Idempotency-Key",
					fmt.Sprintf("api-loadtest-%d", i),
				)

				requestStart := time.Now()

				resp, err := client.Do(req)

				latency := time.Since(requestStart)

				if err != nil {
					mu.Lock()
					errorCount++
					mu.Unlock()
					continue
				}

				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				mu.Lock()

				totalLatency += latency

				if latency < minLatency {
					minLatency = latency
				}

				if latency > maxLatency {
					maxLatency = latency
				}

				if resp.StatusCode == http.StatusOK {
					successCount++
				} else {
					errorCount++
				}

				mu.Unlock()
			}
		}()
	}

	for i := 0; i < totalRequests; i++ {
		requests <- i
	}

	close(requests)

	wg.Wait()

	totalDuration := time.Since(start)

	fmt.Println("========== API LOAD TEST ==========")
	fmt.Printf("Total requests:    %d\n", totalRequests)
	fmt.Printf("Concurrency:       %d\n", concurrency)
	fmt.Printf("Successful:        %d\n", successCount)
	fmt.Printf("Errors:            %d\n", errorCount)
	fmt.Printf("Total duration:    %v\n", totalDuration)
	fmt.Printf("Requests/sec:      %.2f\n",
		float64(totalRequests)/totalDuration.Seconds(),
	)

	if successCount > 0 {
		fmt.Printf("Average latency:   %v\n",
			totalLatency/time.Duration(successCount),
		)
		fmt.Printf("Minimum latency:   %v\n", minLatency)
		fmt.Printf("Maximum latency:   %v\n", maxLatency)
	}

	fmt.Println("===================================")
}