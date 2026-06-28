package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"komodo-customer-api/internal/db"
)

func TestGetPasskeysHandler_Unauthorized(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodGet, "/v1/users//passkeys", nil)
	rr := httptest.NewRecorder()

	svc.GetPasskeysHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAddPasskeyHandler_Unauthorized(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodPost, "/v1/users//passkeys", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	svc.AddPasskeyHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAddPasskeyHandler_BadJSON(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodPost, "/v1/users/{id}/passkeys", strings.NewReader("not-json"))
	req.SetPathValue("id", "user_123")
	rr := httptest.NewRecorder()

	svc.AddPasskeyHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestAddPasskeyHandler_MissingCredentialID(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodPost, "/v1/users/{id}/passkeys", strings.NewReader(`{"public_key":"cG9zZXVkb2tleQ=="}`))
	req.SetPathValue("id", "user_123")
	rr := httptest.NewRecorder()

	svc.AddPasskeyHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdatePasskeyHandler_Unauthorized(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodPatch, "/v1/users//passkeys/cred_abc", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	svc.UpdatePasskeyHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestUpdatePasskeyHandler_MissingCredentialID(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodPatch, "/v1/users/{id}/passkeys/{credential_id}", strings.NewReader(`{}`))
	req.SetPathValue("id", "user_123")
	rr := httptest.NewRecorder()

	svc.UpdatePasskeyHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestDeletePasskeyHandler_Unauthorized(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodDelete, "/v1/users//passkeys/cred_abc", nil)
	rr := httptest.NewRecorder()

	svc.DeletePasskeyHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestDeletePasskeyHandler_MissingCredentialID(t *testing.T) {
	svc := NewService(db.New(nil, nil, ""), ServiceExtraConfig{})
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/{id}/passkeys/{credential_id}", nil)
	req.SetPathValue("id", "user_123")
	rr := httptest.NewRecorder()

	svc.DeletePasskeyHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
