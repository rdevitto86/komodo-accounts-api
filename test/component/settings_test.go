package component_test

import (
	"net/http"
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
)

func TestComponent_Settings_MySettingsReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/settings", nil))

	assertStatus(t, rec, http.StatusNotImplemented, "GET /v1/me/settings")
}
