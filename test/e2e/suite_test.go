//go:build e2e

// Package e2e_test contains end-to-end tests for komodo-accounts-api.
// Tests exercise the full request path: HTTP → handler → address provider (stub or real).
//
// Prerequisites:
//   - Run `just up api` to start LocalStack + the service via docker-compose.
//   - Set ADDRESS_PROVIDER_API_KEY in LocalStack secrets to enable provider-dependent tests.
//
// Run:
//
//	go test -tags=e2e -v ./e2e/
//	make test_e2e
//
// Override target URL:
//
//	BASE_URL=http://localhost:7051 go test -tags=e2e -v ./e2e/
package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

var (
	baseURL string
	client  *http.Client
)

func TestMain(m *testing.M) {
	baseURL = os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:7051"
	}
	client = &http.Client{Timeout: 10 * time.Second}
	os.Exit(m.Run())
}

func makeURL(path string) string {
	return fmt.Sprintf("%s%s", baseURL, path)
}

func get(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, makeURL(path), nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func post(t *testing.T, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, makeURL(path), r)
	if err != nil {
		t.Fatalf("build POST %s: %v", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

func put(t *testing.T, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPut, makeURL(path), r)
	if err != nil {
		t.Fatalf("build PUT %s: %v", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return res
}

func del(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, makeURL(path), nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return res
}

func checkStatus(t *testing.T, res *http.Response, want int) {
	t.Helper()
	if res.StatusCode != want {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("want HTTP %d, got %d\nbody: %s", want, res.StatusCode, body)
	}
}

func decodeJSON(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func authHeader(t *testing.T) map[string]string {
	t.Helper()
	tok := os.Getenv("TEST_JWT")
	if tok == "" {
		t.Skip("TEST_JWT not set — issue a dev JWT via auth-api and set TEST_JWT=<token>")
	}
	return map[string]string{"Authorization": "Bearer " + tok}
}
