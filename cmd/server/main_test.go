package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthCheckMode_NotHealthCheckInvocation_ReturnsFalse(t *testing.T) {
	cases := [][]string{
		{"komodo-accounts-api"},
		{"komodo-accounts-api", "-serve"},
		{"komodo-accounts-api", "--healthcheck"},
	}
	for _, args := range cases {
		code, isHealthCheck := healthCheckMode(args, &config{})
		if isHealthCheck {
			t.Errorf("args %v: expected isHealthCheck=false, got true (code=%d)", args, code)
		}
	}
}

func TestHealthCheckMode_HealthCheckInvocation_NoPortConfigured_ReturnsFailureCode(t *testing.T) {
	code, isHealthCheck := healthCheckMode([]string{"komodo-accounts-api", "-healthcheck"}, &config{})

	if !isHealthCheck {
		t.Fatal("expected isHealthCheck=true for a -healthcheck invocation")
	}
	if code == 0 {
		t.Error("expected a non-zero exit code when neither PORT nor PORT_PRIVATE is configured")
	}
}

func TestHealthCheckMode_HealthCheckInvocation_PublicHealthy_ChecksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	publicPort := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	cfg := &config{publicAddr: ":" + publicPort, privateAddr: ":1"}

	code, isHealthCheck := healthCheckMode([]string{"komodo-accounts-api", "-healthcheck"}, cfg)

	if !isHealthCheck {
		t.Fatal("expected isHealthCheck=true for a -healthcheck invocation")
	}
	if code == 0 {
		t.Error("expected a non-zero exit code when the private probe has nothing listening")
	}
}
