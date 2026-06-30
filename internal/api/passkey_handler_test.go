package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"
)

// ── Unit Tests: GetPasskeysHandler ───────────────────────────────────────────

func TestGetPasskeysHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserPasskeys(gomock.Any(), "user_abc").
		Return([]models.PasskeyCredential{{CredentialID: "cred_1", PublicKey: "pubkey"}}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/users/user_abc/passkeys", nil)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.GetPasskeysHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPasskeysHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/users//passkeys", nil)
	rr := httptest.NewRecorder()
	svc.GetPasskeysHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// ── Unit Tests: AddPasskeyHandler ────────────────────────────────────────────

func TestAddPasskeyHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreatePasskey(gomock.Any(), "user_abc", gomock.Any()).
		Return(nil)

	body := map[string]any{"credential_id": "cred_1", "public_key": "cG9zZXVkb2tleQ=="}
	req := makeRequest(t, http.MethodPost, "/v1/users/user_abc/passkeys", body)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.AddPasskeyHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestAddPasskeyHandler_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreatePasskey(gomock.Any(), "user_abc", gomock.Any()).
		Return(db.ErrPasskeyAlreadyExists)

	body := map[string]any{"credential_id": "cred_1", "public_key": "cG9zZXVkb2tleQ=="}
	req := makeRequest(t, http.MethodPost, "/v1/users/user_abc/passkeys", body)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.AddPasskeyHandler(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestAddPasskeyHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req, _ := http.NewRequest(http.MethodPost, "/v1/users/user_abc/passkeys", strings.NewReader("not-json"))
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.AddPasskeyHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAddPasskeyHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/users//passkeys", map[string]any{"credential_id": "cred_1"})
	rr := httptest.NewRecorder()
	svc.AddPasskeyHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// ── Unit Tests: UpdatePasskeyHandler ─────────────────────────────────────────

func TestUpdatePasskeyHandler_SignCountRegression(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdatePasskey(gomock.Any(), "user_abc", "cred_1", gomock.Any()).
		Return(nil, db.ErrPasskeySignCountRegression)

	body := map[string]any{"sign_count": 1}
	req := makeRequest(t, http.MethodPatch, "/v1/users/user_abc/passkeys/cred_1", body)
	req.SetPathValue("id", "user_abc")
	req.SetPathValue("credential_id", "cred_1")
	rr := httptest.NewRecorder()
	svc.UpdatePasskeyHandler(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestUpdatePasskeyHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdatePasskey(gomock.Any(), "user_abc", "cred_1", gomock.Any()).
		Return(&models.PasskeyCredential{CredentialID: "cred_1"}, nil)

	body := map[string]any{"sign_count": 5}
	req := makeRequest(t, http.MethodPatch, "/v1/users/user_abc/passkeys/cred_1", body)
	req.SetPathValue("id", "user_abc")
	req.SetPathValue("credential_id", "cred_1")
	rr := httptest.NewRecorder()
	svc.UpdatePasskeyHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdatePasskeyHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdatePasskey(gomock.Any(), "user_abc", "cred_missing", gomock.Any()).
		Return(nil, db.ErrNotFound)

	body := map[string]any{"sign_count": 5}
	req := makeRequest(t, http.MethodPatch, "/v1/users/user_abc/passkeys/cred_missing", body)
	req.SetPathValue("id", "user_abc")
	req.SetPathValue("credential_id", "cred_missing")
	rr := httptest.NewRecorder()
	svc.UpdatePasskeyHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── Unit Tests: DeletePasskeyHandler ─────────────────────────────────────────

func TestDeletePasskeyHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeletePasskey(gomock.Any(), "user_abc", "cred_1").
		Return(nil)

	req := makeRequest(t, http.MethodDelete, "/v1/users/user_abc/passkeys/cred_1", nil)
	req.SetPathValue("id", "user_abc")
	req.SetPathValue("credential_id", "cred_1")
	rr := httptest.NewRecorder()
	svc.DeletePasskeyHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestDeletePasskeyHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeletePasskey(gomock.Any(), "user_abc", "cred_missing").
		Return(db.ErrNotFound)

	req := makeRequest(t, http.MethodDelete, "/v1/users/user_abc/passkeys/cred_missing", nil)
	req.SetPathValue("id", "user_abc")
	req.SetPathValue("credential_id", "cred_missing")
	rr := httptest.NewRecorder()
	svc.DeletePasskeyHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
