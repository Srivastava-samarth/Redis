package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const (
	totalRequests = 100
	apiURL        = "http://localhost:8080/urls"
)

func main() {
	client := &http.Client{}

	var wg sync.WaitGroup

	var mu sync.Mutex

	successCount := 0
	rateLimitedCount := 0
	errorCount := 0

	wg.Add(totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func(i int) {
			defer wg.Done()

			req, err := http.NewRequest(
				http.MethodPost,
				apiURL,
				strings.NewReader(`{"url":"https://example.com"}`),
			)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Client-ID", "loadtest-client")
			req.Header.Set(
				"Idempotency-Key",
				fmt.Sprintf("loadtest-%d", i),
			)

			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return
			}

			resp.Body.Close()

			mu.Lock()

			switch resp.StatusCode {
			case http.StatusOK:
				successCount++

			case http.StatusTooManyRequests:
				rateLimitedCount++

			default:
				errorCount++
			}

			mu.Unlock()
		}(i)
	}

	wg.Wait()

	fmt.Printf("Total requests: %d\n", totalRequests)
	fmt.Printf("Concurrency:    %d\n", totalRequests)
	fmt.Printf("Successful:     %d\n", successCount)
	fmt.Printf("Rate limited:   %d\n", rateLimitedCount)
	fmt.Printf("Errors:         %d\n", errorCount)
}
