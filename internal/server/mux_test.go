package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"komodo-accounts-api/internal/api"

	"github.com/rdevitto86/komodo-forge-sdk-go/rules"
)

func TestNewPrivateMux_RegistersExpectedRoutes(t *testing.T) {
	t.Parallel()
	svc := api.NewService(nil, api.ServiceExtraConfig{})
	mux := NewPrivateMux(svc, nil, nil, "")

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
		{http.MethodPost, "/v1/accounts/{id}/communications/unsubscribe-token"},
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
	mux := NewPublicMux(svc, nil, nil, "", nil)

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
		{http.MethodPost, "/v1/me/profile/export"},
		{http.MethodPut, "/v1/me/profile/email"},
		{http.MethodPost, "/v1/me/profile/avatar"},
		{http.MethodDelete, "/v1/me/profile/avatar"},
		{http.MethodGet, "/v1/me/addresses"},
		{http.MethodPost, "/v1/me/addresses"},
		{http.MethodGet, "/v1/me/addresses/{id}"},
		{http.MethodPut, "/v1/me/addresses/{id}"},
		{http.MethodDelete, "/v1/me/addresses/{id}"},
		{http.MethodGet, "/v1/me/payments"},
		{http.MethodPut, "/v1/me/payments"},
		{http.MethodGet, "/v1/me/payments/{id}"},
		{http.MethodDelete, "/v1/me/payments/{id}"},
		{http.MethodGet, "/v1/me/preferences"},
		{http.MethodPut, "/v1/me/preferences"},
		{http.MethodGet, "/v1/me/settings"},
		{http.MethodGet, "/v1/me/consents"},
		{http.MethodPost, "/v1/me/consents"},
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
	mux := NewPublicMux(svc, nil, nil, "", nil)

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
		{http.MethodPost, "/me/profile/export"},
		{http.MethodPut, "/me/profile/email"},
		{http.MethodPost, "/me/profile/avatar"},
		{http.MethodDelete, "/me/profile/avatar"},
		{http.MethodGet, "/me/addresses"},
		{http.MethodPost, "/me/addresses"},
		{http.MethodGet, "/me/addresses/{id}"},
		{http.MethodPut, "/me/addresses/{id}"},
		{http.MethodDelete, "/me/addresses/{id}"},
		{http.MethodGet, "/me/payments"},
		{http.MethodPut, "/me/payments"},
		{http.MethodGet, "/me/payments/{id}"},
		{http.MethodDelete, "/me/payments/{id}"},
		{http.MethodGet, "/me/preferences"},
		{http.MethodPut, "/me/preferences"},
		{http.MethodGet, "/me/settings"},
		{http.MethodGet, "/me/consents"},
		{http.MethodPost, "/me/consents"},
	}

	for _, r := range ruleValidated {
		if rule, _ := rules.GetRule(r.path, r.method); rule == nil {
			t.Errorf("route %s %s has no matching validation rule", r.method, r.path)
		}
	}
}
