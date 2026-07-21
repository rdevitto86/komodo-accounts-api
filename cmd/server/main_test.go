package main

import (
	"testing"

	sdkapi "github.com/rdevitto86/komodo-forge-sdk-go/api"
)

func TestHealthCheckMode_NotHealthCheckInvocation_ReturnsFalse(t *testing.T) {
	cases := [][]string{
		{"komodo-accounts-api"},
		{"komodo-accounts-api", "-serve"},
		{"komodo-accounts-api", "--healthcheck"},
	}
	for _, args := range cases {
		code, isHealthCheck := healthCheckMode(args)
		if isHealthCheck {
			t.Errorf("args %v: expected isHealthCheck=false, got true (code=%d)", args, code)
		}
	}
}

func TestHealthCheckMode_HealthCheckInvocation_NoPortConfigured_ReturnsFailureCode(t *testing.T) {
	t.Setenv(sdkapi.PORT, "")
	t.Setenv(sdkapi.PORT_PRIVATE, "")

	code, isHealthCheck := healthCheckMode([]string{"komodo-accounts-api", "-healthcheck"})

	if !isHealthCheck {
		t.Fatal("expected isHealthCheck=true for a -healthcheck invocation")
	}
	if code == 0 {
		t.Error("expected a non-zero exit code when neither PORT nor PORT_PRIVATE is configured")
	}
}
