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

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"
)

// ── Unit Tests: GetProfileHandler ────────────────────────────────────────────

func TestGetProfileHandler_FoundViaPathID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUser(gomock.Any(), "user_abc").
		Return(&models.User{CustomerID: "user_abc"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/users/user_abc/profile", nil)
	req.SetPathValue("id", "user_abc")
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
		GetUser(gomock.Any(), "user_missing").
		Return(nil, db.ErrNotFound)

	req := makeRequest(t, http.MethodGet, "/v1/users/user_missing/profile", nil)
	req.SetPathValue("id", "user_missing")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetProfileHandler_IDORResolvesPathID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUser(gomock.Any(), "path_user_id").
		Return(&models.User{CustomerID: "path_user_id"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/users/path_user_id/profile", nil)
	req.SetPathValue("id", "path_user_id")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

// ── Unit Tests: CreateUserHandler ────────────────────────────────────────────

func TestCreateUserHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(nil)

	body := map[string]any{"email": "test@example.com", "first_name": "Test", "last_name": "User"}
	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateUserHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateUserHandler_NoJWT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/me/profile", map[string]any{"email": "test@example.com"})
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestCreateUserHandler_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to create user: %w", db.ErrAlreadyExists))

	body := map[string]any{"email": "taken@example.com", "first_name": "Test", "last_name": "User"}
	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestCreateUserHandler_WritesSettingsRow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	body := map[string]any{"email": "new@example.com", "first_name": "New", "last_name": "User"}
	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "user_new")
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateUserHandler_UnknownField_Returns400(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{
		"email":      "test@example.com",
		"first_name": "Test",
		"last_name":  "User",
		"bogus":      "field",
	}
	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.CreateUserHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Unit Tests: UpdateProfileHandler ─────────────────────────────────────────

func TestUpdateProfileHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateUser(gomock.Any(), "user_abc", gomock.Any()).
		Return(&models.User{CustomerID: "user_abc", FirstName: "Foo"}, nil)

	req := withUserID(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateProfileHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateUser(gomock.Any(), "user_missing", gomock.Any()).
		Return(nil, fmt.Errorf("wrapped: %w", db.ErrNotFound))

	req := withUserID(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "user_missing")
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

	repo.EXPECT().SoftDeleteCustomer(gomock.Any(), "user_abc").Return(nil)
	repo.EXPECT().GetUser(gomock.Any(), "user_abc").Return(nil, db.ErrNotFound)

	req := withUserID(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "user_abc")
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

	creds := &models.CredentialsResponse{CustomerID: "user_abc", PasswordHash: "hash"}
	repo.EXPECT().GetUserCredentialsByEmail(gomock.Any(), "user@test.com").Return(creds, nil).Times(2)
	repo.EXPECT().GetUser(gomock.Any(), "user_abc").Return(&models.User{CustomerID: "user_abc", Email: "user@test.com"}, nil)
	repo.EXPECT().DeleteUser(gomock.Any(), "user_abc").Return(nil)

	ctx := context.Background()

	got, err := svc.GetCredentials(ctx, "user@test.com")
	require.NoError(t, err)
	require.Equal(t, creds.PasswordHash, got.PasswordHash)

	err = svc.DeleteProfile(ctx, "user_abc")
	require.NoError(t, err)

	got2, err := svc.GetCredentials(ctx, "user@test.com")
	require.NoError(t, err)
	require.Equal(t, creds.PasswordHash, got2.PasswordHash)
}

func TestDeleteProfileHandler_RepoFailureSurfaces5xx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().SoftDeleteCustomer(gomock.Any(), "user_abc").Return(errors.New("dynamodb unavailable"))

	req := withUserID(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
