//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
)

func TestHealth(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/health", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusOK)
}

func TestGetProfile_NoAuth(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/profile", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

func TestGetProfile(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/profile", authHeader(t))
	defer res.Body.Close()
	checkStatus(t, res, http.StatusOK)

	var profile struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	decodeJSON(t, res, &profile)
	if profile.ID == "" {
		t.Fatal("expected non-empty id in profile response")
	}
}

func TestUpdateProfile(t *testing.T) {
	testutil.E2E(t)
	h := authHeader(t)
	res := put(t, "/me/profile", map[string]any{"display_name": "E2E Test Account"}, h)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusOK)
}

func TestAddresses_NoAuth(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/addresses", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

func TestAddresses_List(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/addresses", authHeader(t))
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("addresses repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, res, http.StatusOK)
}

func TestAddresses_AddAndDelete(t *testing.T) {
	testutil.E2E(t)
	h := authHeader(t)

	addr := map[string]any{
		"line1":   "123 E2E Lane",
		"city":    "Testville",
		"state":   "CA",
		"zip":     "90001",
		"country": "US",
		"label":   "home",
	}
	addResp := post(t, "/me/addresses", addr, h)
	defer addResp.Body.Close()
	if addResp.StatusCode == http.StatusNotImplemented {
		t.Skip("addresses repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, addResp, http.StatusCreated)

	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, addResp, &created)
	if created.ID == "" {
		t.Fatal("expected non-empty id in add-address response")
	}

	delResp := del(t, "/me/addresses/"+created.ID, h)
	defer delResp.Body.Close()
	checkStatus(t, delResp, http.StatusOK)
}

func TestPayments_NoAuth(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/payments", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

func TestPayments_List(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/payments", authHeader(t))
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("payments repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, res, http.StatusOK)
}

func TestPreferences_NoAuth(t *testing.T) {
	testutil.E2E(t)
	res := get(t, "/me/preferences", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

func TestPreferences_GetAndUpdate(t *testing.T) {
	testutil.E2E(t)
	h := authHeader(t)

	getResp := get(t, "/me/preferences", h)
	defer getResp.Body.Close()
	if getResp.StatusCode == http.StatusNotImplemented {
		t.Skip("preferences repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, getResp, http.StatusOK)

	putResp := put(t, "/me/preferences",
		map[string]any{"marketing_emails": false, "theme": "dark"},
		h,
	)
	defer putResp.Body.Close()
	checkStatus(t, putResp, http.StatusOK)
}

func TestPreferences_Delete(t *testing.T) {
	testutil.E2E(t)
	h := authHeader(t)
	res := del(t, "/me/preferences", h)
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("preferences repo not yet wired")
	}
	checkStatus(t, res, http.StatusOK)
}
