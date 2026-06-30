package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/models"
)

// ── Unit Tests: GetCredentialsHandler ────────────────────────────────────────

func TestGetCredentialsHandler_Found(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			CustomerID:    "cust_1",
			PasswordHash:  "hashed_password",
			EmailVerified: true,
			AuthMethods:   []string{"password"},
		}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/credentials?email=user@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.CredentialsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "hashed_password", resp.PasswordHash)
	assert.Equal(t, "cust_1", resp.CustomerID)
}

func TestGetCredentialsHandler_NoEmail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/credentials", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Unit Tests: UpdateCredentialsHandler ─────────────────────────────────────

func TestUpdateCredentialsHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateUserCredentials(gomock.Any(), "user_abc", gomock.Any()).
		Return(nil)

	body := map[string]any{"password_hash": "new_hash"}
	req := makeRequest(t, http.MethodPut, "/v1/users/user_abc/credentials", body)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestUpdateCredentialsHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/users/user_abc/credentials", nil)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateCredentialsHandler_NoPathID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/users//credentials", map[string]any{"password_hash": "hash"})
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateCredentialsHandler_EmptyBodyPasswordWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/users/user_abc/credentials", nil)
	req.SetPathValue("id", "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetCredentialsHandler_EmailVerifiedFromSettings_MissingSettings(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			CustomerID:    "cust_1",
			EmailVerified: false,
			AuthMethods:   []string{},
		}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/credentials?email=user@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.CredentialsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.EmailVerified)
}

func TestGetCredentialsHandler_EmailVerifiedFromSettings_Verified(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			CustomerID:    "cust_1",
			EmailVerified: true,
			AuthMethods:   []string{"passkey"},
		}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/credentials?email=user@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.CredentialsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.EmailVerified)
}

// ── Unit Tests: GetUserExistsHandler ─────────────────────────────────────────

func TestGetUserExistsHandler_Found(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserExistsByEmail(gomock.Any(), "exists@example.com").
		Return(&models.UserExistsResponse{Exists: true, AuthMethods: []string{"password"}}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/users/exists?email=exists@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetUserExistsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.UserExistsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.Exists)
}

func TestGetUserExistsHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserExistsByEmail(gomock.Any(), "nobody@example.com").
		Return(&models.UserExistsResponse{Exists: false, AuthMethods: []string{}}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/users/exists?email=nobody@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetUserExistsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.UserExistsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.Exists)
}
