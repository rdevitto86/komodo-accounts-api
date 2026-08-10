package component_test

import (
	"net/http"
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
)

func TestComponent_Consents_ListReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/consents", nil))

	assertStatus(t, rec, http.StatusNotImplemented, "GET /v1/me/consents")
}

func TestComponent_Consents_RecordReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	body := map[string]any{"channel": "email", "action": "opt_in"}
	rec := serve(h.publicMux, h.signedIn(t, http.MethodPost, "/v1/me/consents", body))

	assertStatus(t, rec, http.StatusNotImplemented, "POST /v1/me/consents")
}
