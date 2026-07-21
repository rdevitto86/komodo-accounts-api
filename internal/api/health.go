package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Health check function that checks server health
func HealthProbe(addr string) int {
	port := strings.TrimPrefix(strings.TrimSpace(addr), ":")
	if port == "" {
		fmt.Println("healthcheck failed: no port configured")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost:%s/health", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Printf("healthcheck failed: %v\n", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("healthcheck failed: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("healthcheck failed: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
