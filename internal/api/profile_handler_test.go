package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

func TestGetProfileHandler_FoundViaPathID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccount(gomock.Any(), "account_abc").
		Return(&models.Account{AccountID: "account_abc"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/account_abc/profile", nil)
	req.SetPathValue("id", "account_abc")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetProfileHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/me/profile", nil)
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestGetProfileHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccount(gomock.Any(), "account_missing").
		Return(nil, db.ErrNotFound)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/account_missing/profile", nil)
	req.SetPathValue("id", "account_missing")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetProfileHandler_IDORResolvesPathID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccount(gomock.Any(), "path_account_id").
		Return(&models.Account{AccountID: "path_account_id"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/path_account_id/profile", nil)
	req.SetPathValue("id", "path_account_id")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestCreateAccountHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateAccount(gomock.Any(), gomock.Any()).Return(nil)

	body := map[string]any{"email": "test@example.com", "first_name": "Test", "last_name": "Account"}
	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateAccountHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateAccountHandler_NoJWT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/me/profile", map[string]any{"email": "test@example.com"})
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestCreateAccountHandler_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateAccount(gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to create account: %w", db.ErrAlreadyExists))

	body := map[string]any{"email": "taken@example.com", "first_name": "Test", "last_name": "Account"}
	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestCreateAccountHandler_WritesSettingsRow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateAccount(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	body := map[string]any{"email": "new@example.com", "first_name": "New", "last_name": "Account"}
	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_new")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateAccountHandler_UnknownField_Returns400(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{
		"email":      "test@example.com",
		"first_name": "Test",
		"last_name":  "Account",
		"bogus":      "field",
	}
	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Unit Tests: UpdateProfileHandler ─────────────────────────────────────────

func TestUpdateProfileHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccount(gomock.Any(), "account_abc", gomock.Any()).
		Return(&models.Account{AccountID: "account_abc", FirstName: "Foo"}, nil)

	req := withAccountID(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateProfileHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccount(gomock.Any(), "account_missing", gomock.Any()).
		Return(nil, fmt.Errorf("wrapped: %w", db.ErrNotFound))

	req := withAccountID(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "account_missing")
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdateProfileHandler_NoJWT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"})
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// ── Unit Tests: DeleteProfileHandler ─────────────────────────────────────────

func TestDeleteProfileHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().SoftDeleteAccount(gomock.Any(), "account_abc").Return(nil)
	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(nil, db.ErrNotFound)

	req := withAccountID(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)
}

func TestDeleteProfileHandler_NoJWT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodDelete, "/v1/me/profile", nil)
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestDeleteProfileHandler_InvalidatesCredentialsCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	creds := &models.CredentialsResponse{AccountID: "account_abc", PasswordHash: "hash"}
	repo.EXPECT().GetAccountCredentialsByEmail(gomock.Any(), "user@test.com").Return(creds, nil).Times(2)
	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(&models.Account{AccountID: "account_abc", Email: "user@test.com"}, nil)
	repo.EXPECT().DeleteAccount(gomock.Any(), "account_abc").Return(nil)

	ctx := context.Background()

	got, err := svc.GetCredentials(ctx, "user@test.com")
	require.NoError(t, err)
	require.Equal(t, creds.PasswordHash, got.PasswordHash)

	err = svc.DeleteProfile(ctx, "account_abc")
	require.NoError(t, err)

	got2, err := svc.GetCredentials(ctx, "user@test.com")
	require.NoError(t, err)
	require.Equal(t, creds.PasswordHash, got2.PasswordHash)
}

func TestDeleteProfileHandler_RepoFailureSurfaces5xx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().SoftDeleteAccount(gomock.Any(), "account_abc").Return(errors.New("dynamodb unavailable"))

	req := withAccountID(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
