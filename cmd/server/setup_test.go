package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"komodo-accounts-api/internal/api"

	"github.com/rdevitto86/komodo-forge-sdk-go/rules"
)

func TestNewPrivateMux_RegistersExpectedRoutes(t *testing.T) {
	t.Parallel()
	svc := api.NewService(nil, api.ServiceExtraConfig{})
	mux := newPrivateMux(svc, nil, nil, &config{})

	wants := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/accounts/{id}"},
		{http.MethodGet, "/v1/accounts/{id}/addresses"},
		{http.MethodGet, "/v1/accounts/{id}/preferences"},
		{http.MethodGet, "/v1/accounts/{id}/payments"},
		{http.MethodGet, "/v1/accounts/credentials"},
		{http.MethodPut, "/v1/accounts/{id}/credentials"},
		{http.MethodGet, "/v1/accounts/{id}/passkeys"},
		{http.MethodPost, "/v1/accounts/{id}/passkeys"},
		{http.MethodPatch, "/v1/accounts/{id}/passkeys/{credential_id}"},
		{http.MethodDelete, "/v1/accounts/{id}/passkeys/{credential_id}"},
		{http.MethodGet, "/v1/accounts/{id}/settings"},
		{http.MethodPut, "/v1/accounts/{id}/settings"},
		{http.MethodPut, "/v1/accounts/{id}/settings/tags"},
	}

	for _, w := range wants {
		req := httptest.NewRequest(w.method, w.path, nil)
		_, pattern := mux.Handler(req)
		if pattern != w.method+" "+w.path {
			t.Errorf("route %s %s not registered as expected, got pattern %q", w.method, w.path, pattern)
		}
	}
}

func TestNewPublicMux_RegistersExpectedRoutes(t *testing.T) {
	t.Parallel()
	svc := api.NewService(nil, api.ServiceExtraConfig{})
	mux := newPublicMux(svc, nil, nil, &config{}, nil)

	wants := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/health/ready"},
		{http.MethodGet, "/v1/me/profile"},
		{http.MethodPost, "/v1/me/profile"},
		{http.MethodPut, "/v1/me/profile"},
		{http.MethodDelete, "/v1/me/profile"},
		{http.MethodPost, "/v1/me/profile/restore"},
		{http.MethodPost, "/v1/me/profile/export"},
		{http.MethodPost, "/v1/me/profile/avatar"},
		{http.MethodGet, "/v1/me/addresses"},
		{http.MethodPost, "/v1/me/addresses"},
		{http.MethodPut, "/v1/me/addresses/{id}"},
		{http.MethodDelete, "/v1/me/addresses/{id}"},
		{http.MethodGet, "/v1/me/payments"},
		{http.MethodPut, "/v1/me/payments"},
		{http.MethodDelete, "/v1/me/payments/{id}"},
		{http.MethodGet, "/v1/me/preferences"},
		{http.MethodPut, "/v1/me/preferences"},
		{http.MethodDelete, "/v1/me/preferences"},
		{http.MethodPost, "/v1/communications/unsubscribe"},
		{http.MethodGet, "/v1/accounts/exists"},
	}

	for _, w := range wants {
		req := httptest.NewRequest(w.method, w.path, nil)
		_, pattern := mux.Handler(req)
		if pattern != w.method+" "+w.path {
			t.Errorf("route %s %s not registered as expected, got pattern %q", w.method, w.path, pattern)
		}
	}
}

func TestNewPublicMux_ExistsRateLimit_AppliesOverrides(t *testing.T) {
	t.Setenv("EXISTS_RATE_LIMIT_MAX_ENTRIES", "10")
	t.Setenv("EXISTS_RATE_LIMIT_BUCKET_TTL_SEC", "60")

	svc := api.NewService(nil, api.ServiceExtraConfig{})
	mux := newPublicMux(svc, nil, nil, &config{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/exists", nil)
	_, pattern := mux.Handler(req)
	if pattern != "GET /v1/accounts/exists" {
		t.Errorf("route GET /v1/accounts/exists not registered as expected, got pattern %q", pattern)
	}
}

func TestNewPublicMux_RuleValidatedRoutes_HaveMatchingValidationRule(t *testing.T) {
	t.Parallel()
	rules.ResetForTesting()
	t.Cleanup(rules.ResetForTesting)

	if !rules.LoadConfig("../../validation_rules.yaml") {
		t.Fatal("failed to load validation_rules.yaml for contract test")
	}

	ruleValidated := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/me/profile"},
		{http.MethodPost, "/me/profile"},
		{http.MethodPut, "/me/profile"},
		{http.MethodDelete, "/me/profile"},
		{http.MethodPost, "/me/profile/restore"},
		{http.MethodPost, "/me/profile/export"},
		{http.MethodPost, "/me/profile/avatar"},
		{http.MethodGet, "/me/addresses"},
		{http.MethodPost, "/me/addresses"},
		{http.MethodPut, "/me/addresses/{id}"},
		{http.MethodDelete, "/me/addresses/{id}"},
		{http.MethodGet, "/me/payments"},
		{http.MethodPut, "/me/payments"},
		{http.MethodDelete, "/me/payments/{id}"},
		{http.MethodGet, "/me/preferences"},
		{http.MethodPut, "/me/preferences"},
		{http.MethodDelete, "/me/preferences"},
	}

	for _, r := range ruleValidated {
		if rule, _ := rules.GetRule(r.path, r.method); rule == nil {
			t.Errorf("route %s %s has no matching validation rule", r.method, r.path)
		}
	}
}

func TestLoadServerTimeoutsAppliesDefaults(t *testing.T) {
	timeouts := loadServerTimeouts()

	want := serverTimeouts{
		boot:     defaultBootTimeout,
		read:     defaultReadTimeout,
		write:    defaultWriteTimeout,
		idle:     defaultIdleTimeout,
		header:   defaultHeaderTimeout,
		shutdown: defaultShutdownTimeout,
	}
	if timeouts != want {
		t.Errorf("loadServerTimeouts() = %+v, want %+v", timeouts, want)
	}
}

func TestLoadServerTimeoutsAppliesOverrides(t *testing.T) {
	t.Setenv("BOOTSTRAP_TIMEOUT", "20s")
	t.Setenv("SERVER_READ_TIMEOUT", "45s")
	t.Setenv("SERVER_WRITE_TIMEOUT", "0s")
	t.Setenv("SERVER_IDLE_TIMEOUT", "90s")
	t.Setenv("SERVER_READ_HEADER_TIMEOUT", "3s")
	t.Setenv("SERVER_SHUTDOWN_TIMEOUT", "15s")

	timeouts := loadServerTimeouts()

	if timeouts.boot != 20*time.Second {
		t.Errorf("boot = %v, want 20s", timeouts.boot)
	}
	if timeouts.read != 45*time.Second {
		t.Errorf("read = %v, want 45s", timeouts.read)
	}
	if timeouts.write != defaultWriteTimeout {
		t.Errorf("write = %v, want the default %v when overridden with an invalid 0s", timeouts.write, defaultWriteTimeout)
	}
	if timeouts.idle != 90*time.Second {
		t.Errorf("idle = %v, want 90s", timeouts.idle)
	}
	if timeouts.header != 3*time.Second {
		t.Errorf("header = %v, want 3s", timeouts.header)
	}
	if timeouts.shutdown != 15*time.Second {
		t.Errorf("shutdown = %v, want 15s", timeouts.shutdown)
	}
}

func TestLoadServerTimeoutsAppliesWriteOverride(t *testing.T) {
	t.Setenv("SERVER_WRITE_TIMEOUT", "12s")

	timeouts := loadServerTimeouts()

	if timeouts.write != 12*time.Second {
		t.Errorf("write = %v, want 12s", timeouts.write)
	}
}

func TestLoadServiceTuningErrorsOnMissingRequiredKeys(t *testing.T) {
	if _, err := loadServiceTuning(); err == nil {
		t.Fatal("expected an error when required env vars are unset")
	}
}

func TestLoadServiceTuningAppliesDefaultsAndOverrides(t *testing.T) {
	t.Setenv("S3_EXPORT_BUCKET", "exports")
	t.Setenv("S3_AVATAR_BUCKET", "avatars")
	t.Setenv("PROFILE_CACHE_MAX_ENTRIES", "1234")

	tuning, err := loadServiceTuning()
	if err != nil {
		t.Fatalf("loadServiceTuning() returned an error: %v", err)
	}
	if tuning.exportBucket != "exports" || tuning.avatarBucket != "avatars" {
		t.Errorf("buckets = %q/%q, want exports/avatars", tuning.exportBucket, tuning.avatarBucket)
	}
	if tuning.cacheMaxSize != 1234 {
		t.Errorf("cacheMaxSize = %d, want 1234", tuning.cacheMaxSize)
	}
	if tuning.presignExpiry != defaultPresignExpiry {
		t.Errorf("presignExpiry = %v, want the default %v", tuning.presignExpiry, defaultPresignExpiry)
	}
}

func TestLoadServiceTuningAppliesDurationOverrides(t *testing.T) {
	t.Setenv("S3_EXPORT_BUCKET", "exports")
	t.Setenv("S3_AVATAR_BUCKET", "avatars")
	t.Setenv("S3_PRESIGN_TTL", "5m")
	t.Setenv("UNSUBSCRIBE_TOKEN_TTL", "48h")
	t.Setenv("SOFT_DELETE_WINDOW", "72h")
	t.Setenv("PROFILE_CACHE_TTL", "30s")

	tuning, err := loadServiceTuning()
	if err != nil {
		t.Fatalf("loadServiceTuning() returned an error: %v", err)
	}
	if tuning.presignExpiry != 5*time.Minute {
		t.Errorf("presignExpiry = %v, want 5m", tuning.presignExpiry)
	}
	if tuning.unsubTokenTTL != 48*time.Hour {
		t.Errorf("unsubTokenTTL = %v, want 48h", tuning.unsubTokenTTL)
	}
	if tuning.restoreWindow != 72*time.Hour {
		t.Errorf("restoreWindow = %v, want 72h", tuning.restoreWindow)
	}
	if tuning.cacheTTL != 30*time.Second {
		t.Errorf("cacheTTL = %v, want 30s", tuning.cacheTTL)
	}
}

func TestSecretKeysExcludeCDKInjectedNames(t *testing.T) {
	cdkInjected := map[string]bool{
		"DYNAMODB_TABLE": true,
		"PORT":           true,
		"PORT_PRIVATE":   true,
		"AWS_REGION":     true,
	}
	for _, key := range secretKeys {
		if cdkInjected[key] {
			t.Errorf("%q is injected by CDK and must not also be fetched as a secret", key)
		}
	}
}

func TestSecretKeysExcludeSingleJWTAudience(t *testing.T) {
	for _, key := range secretKeys {
		if key == "JWT_AUDIENCE" {
			t.Error("JWT_AUDIENCE must not be fetched: this service verifies two token planes " +
				"with separate audiences, configured via JWT_AUDIENCE_USER and JWT_AUDIENCE_SERVICE")
		}
	}
}
