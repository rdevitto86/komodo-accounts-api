//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
)

func TestHealth(t *testing.T) {
	res := get(t, "/health", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusOK)
}

// --- Profile ---

// TestGetProfile_NoAuth verifies unauthenticated requests are rejected.
func TestGetProfile_NoAuth(t *testing.T) {
	res := get(t, "/me/profile", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

// TestGetProfile fetches the authenticated user's profile.
func TestGetProfile(t *testing.T) {
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

// TestUpdateProfile updates a mutable profile field.
func TestUpdateProfile(t *testing.T) {
	h := authHeader(t)
	res := put(t, "/me/profile", map[string]any{"display_name": "E2E Test User"}, h)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusOK)
}

// --- Addresses ---

// TestAddresses_NoAuth verifies address routes require auth.
func TestAddresses_NoAuth(t *testing.T) {
	res := get(t, "/me/addresses", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

// TestAddresses_List fetches the authenticated user's saved addresses.
// Returns 501 if DynamoDB schema is not yet wired.
func TestAddresses_List(t *testing.T) {
	res := get(t, "/me/addresses", authHeader(t))
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("addresses repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, res, http.StatusOK)
}

// TestAddresses_AddAndDelete adds a new address then deletes it.
func TestAddresses_AddAndDelete(t *testing.T) {
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

// --- Payment methods ---

// TestPayments_NoAuth verifies payment routes require auth.
func TestPayments_NoAuth(t *testing.T) {
	res := get(t, "/me/payments", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

// TestPayments_List fetches the authenticated user's saved payment methods.
func TestPayments_List(t *testing.T) {
	res := get(t, "/me/payments", authHeader(t))
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("payments repo not yet wired — finalize DynamoDB schema to enable")
	}
	checkStatus(t, res, http.StatusOK)
}

// --- Preferences ---

// TestPreferences_NoAuth verifies preference routes require auth.
func TestPreferences_NoAuth(t *testing.T) {
	res := get(t, "/me/preferences", nil)
	defer res.Body.Close()
	checkStatus(t, res, http.StatusUnauthorized)
}

// TestPreferences_GetAndUpdate reads then updates user preferences.
func TestPreferences_GetAndUpdate(t *testing.T) {
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

// TestPreferences_Delete removes the authenticated user's preferences.
func TestPreferences_Delete(t *testing.T) {
	h := authHeader(t)
	res := del(t, "/me/preferences", h)
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotImplemented {
		t.Skip("preferences repo not yet wired")
	}
	checkStatus(t, res, http.StatusOK)
}
