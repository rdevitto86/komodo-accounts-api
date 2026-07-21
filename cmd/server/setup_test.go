package main

import (
	"net/http"
	"reflect"
	"testing"

	"komodo-accounts-api/internal/api"

	"github.com/rdevitto86/komodo-forge-sdk-go/rules"
)

func TestVersioned(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {}
	chain := []func(http.Handler) http.Handler{func(h http.Handler) http.Handler { return h }}

	routes := versioned(http.MethodGet, "/me/profile", handler, chain, "v1", "v2")

	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	wantPaths := []string{"/v1/me/profile", "/v2/me/profile"}
	for i, r := range routes {
		if r.method != http.MethodGet {
			t.Errorf("route[%d].method = %q, want %q", i, r.method, http.MethodGet)
		}
		if r.path != wantPaths[i] {
			t.Errorf("route[%d].path = %q, want %q", i, r.path, wantPaths[i])
		}
		if reflect.ValueOf(r.handler).Pointer() != reflect.ValueOf(http.HandlerFunc(handler)).Pointer() {
			t.Errorf("route[%d].handler does not match input handler", i)
		}
		if len(r.chain) != len(chain) {
			t.Fatalf("route[%d].chain length = %d, want %d", i, len(r.chain), len(chain))
		}
		for j := range chain {
			if reflect.ValueOf(r.chain[j]).Pointer() != reflect.ValueOf(chain[j]).Pointer() {
				t.Errorf("route[%d].chain[%d] does not match input chain element", i, j)
			}
		}
	}
}

func TestVersionedPaths(t *testing.T) {
	paths := versionedPaths("/me/profile", "v1", "v2")

	want := []string{"/v1/me/profile", "/v2/me/profile"}
	if len(paths) != len(want) {
		t.Fatalf("expected %d paths, got %d", len(want), len(paths))
	}
	for i, p := range paths {
		if p != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestPrivateRoutes(t *testing.T) {
	svc := api.NewService(nil, api.ServiceExtraConfig{})
	internal := []func(http.Handler) http.Handler{func(h http.Handler) http.Handler { return h }}

	routes := privateRoutes(svc, internal)

	wants := []struct {
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{http.MethodGet, "/v1/accounts/{id}", svc.GetProfileHandler},
		{http.MethodGet, "/v1/accounts/{id}/addresses", svc.GetAddressesHandler},
		{http.MethodGet, "/v1/accounts/{id}/preferences", svc.GetPreferencesHandler},
		{http.MethodGet, "/v1/accounts/{id}/payments", svc.GetPaymentsHandler},
		{http.MethodGet, "/v1/accounts/credentials", svc.GetCredentialsHandler},
		{http.MethodPut, "/v1/accounts/{id}/credentials", svc.UpdateCredentialsHandler},
		{http.MethodGet, "/v1/accounts/{id}/passkeys", svc.GetPasskeysHandler},
		{http.MethodPost, "/v1/accounts/{id}/passkeys", svc.AddPasskeyHandler},
		{http.MethodPatch, "/v1/accounts/{id}/passkeys/{credential_id}", svc.UpdatePasskeyHandler},
		{http.MethodDelete, "/v1/accounts/{id}/passkeys/{credential_id}", svc.DeletePasskeyHandler},
		{http.MethodGet, "/v1/accounts/{id}/settings", svc.GetSettingsHandler},
		{http.MethodPut, "/v1/accounts/{id}/settings", svc.UpdateSettingsHandler},
		{http.MethodPut, "/v1/accounts/{id}/settings/tags", svc.UpdateSettingsTagsHandler},
	}

	if len(routes) != len(wants) {
		t.Fatalf("expected %d routes, got %d", len(wants), len(routes))
	}

	for i, r := range routes {
		if r.method != wants[i].method {
			t.Errorf("route[%d].method = %q, want %q", i, r.method, wants[i].method)
		}
		if r.path != wants[i].path {
			t.Errorf("route[%d].path = %q, want %q", i, r.path, wants[i].path)
		}
		if reflect.ValueOf(r.handler).Pointer() != reflect.ValueOf(wants[i].handler).Pointer() {
			t.Errorf("route[%d].handler does not match expected handler for %s %s", i, r.method, r.path)
		}
		if len(r.chain) != len(internal) {
			t.Fatalf("route[%d].chain length = %d, want %d", i, len(r.chain), len(internal))
		}
		for j := range internal {
			if reflect.ValueOf(r.chain[j]).Pointer() != reflect.ValueOf(internal[j]).Pointer() {
				t.Errorf("route[%d].chain[%d] does not match input chain element", i, j)
			}
		}
	}
}

func TestPublicRuleValidatedRoutes_EveryRoute_HasMatchingValidationRule(t *testing.T) {
	rules.ResetForTesting()
	t.Cleanup(rules.ResetForTesting)

	if !rules.LoadConfig("../../validation_rules.yaml") {
		t.Fatal("failed to load validation_rules.yaml for contract test")
	}

	svc := api.NewService(nil, api.ServiceExtraConfig{})

	for _, r := range publicRuleValidatedRoutes(svc, nil, nil) {
		if rule, _ := rules.GetRule(r.path, r.method); rule == nil {
			t.Errorf("route %s %s has no matching validation rule", r.method, r.path)
		}
	}
}
