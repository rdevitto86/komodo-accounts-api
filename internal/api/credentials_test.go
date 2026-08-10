package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/models"
)

func TestGetCredentialsHandler_Found(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			AccountId:     "cust_1",
			PasswordHash:  ptr("hashed_password"),
			EmailVerified: true,
			AuthMethods:   []models.CredentialsResponseAuthMethods{"password"},
		}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/credentials?email=user@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.CredentialsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "hashed_password", deref(resp.PasswordHash))
	assert.Equal(t, "cust_1", resp.AccountId)
}

func TestGetCredentialsHandler_NoEmail(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/credentials", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateCredentialsHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccountCredentials(gomock.Any(), "account_abc", gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetAccount(gomock.Any(), "account_abc").
		Return(&models.Account{AccountId: "account_abc", Email: "USER@Example.com"}, nil)

	body := map[string]any{"password_hash": "new_hash"}
	req := makeRequest(t, http.MethodPut, "/v1/accounts/account_abc/credentials", body)
	req.SetPathValue("id", "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestUpdateCredentialsHandler_BadJSON(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/accounts/account_abc/credentials", nil)
	req.SetPathValue("id", "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateCredentialsHandler_NoPathID(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/accounts//credentials", map[string]any{"password_hash": "hash"})
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateCredentialsHandler_EmptyBodyPasswordWrite(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/accounts/account_abc/credentials", nil)
	req.SetPathValue("id", "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdateCredentialsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetCredentialsHandler_EmailVerifiedFromSettings_MissingSettings(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			AccountId:     "cust_1",
			EmailVerified: false,
			AuthMethods:   []models.CredentialsResponseAuthMethods{},
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
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{
			AccountId:     "cust_1",
			EmailVerified: true,
			AuthMethods:   []models.CredentialsResponseAuthMethods{"passkey"},
		}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/credentials?email=user@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetCredentialsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.CredentialsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.EmailVerified)
}

func TestGetAccountExistsHandler_Found(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountExistsByEmail(gomock.Any(), "exists@example.com").
		Return(&models.AccountExistsResponse{Exists: true}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/exists?email=exists@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetAccountExistsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.AccountExistsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.Exists)
	assert.NotContains(t, rr.Body.String(), "auth_methods")
}

func TestGetAccountExistsHandler_NotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountExistsByEmail(gomock.Any(), "nobody@example.com").
		Return(&models.AccountExistsResponse{Exists: false}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/exists?email=nobody@example.com", nil)
	rr := httptest.NewRecorder()
	svc.GetAccountExistsHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp models.AccountExistsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.Exists)
}

func TestGetCredentials_CacheKeyIsLowercased(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "USER@Example.com").
		Return(&models.CredentialsResponse{AccountId: "cust_1"}, nil).
		Times(1)

	_, err := svc.GetCredentials(context.Background(), "USER@Example.com")
	require.NoError(t, err)

	creds, err := svc.GetCredentials(context.Background(), "user@example.com")
	require.NoError(t, err)
	assert.Equal(t, "cust_1", creds.AccountId)
}

func TestUpdateCredentials_InvalidatesCacheAfterSuccess(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	oldHash := "old_hash"
	newHash := "new_hash"
	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{AccountId: "cust_1", PasswordHash: &oldHash}, nil).
		Times(1)
	repo.EXPECT().
		UpdateAccountCredentials(gomock.Any(), "cust_1", gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetAccount(gomock.Any(), "cust_1").
		Return(&models.Account{AccountId: "cust_1", Email: "USER@Example.com"}, nil)
	repo.EXPECT().
		GetAccountCredentialsByEmail(gomock.Any(), "user@example.com").
		Return(&models.CredentialsResponse{AccountId: "cust_1", PasswordHash: &newHash}, nil).
		Times(1)

	_, err := svc.GetCredentials(context.Background(), "user@example.com")
	require.NoError(t, err)

	err = svc.UpdateCredentials(context.Background(), "cust_1", &models.UpdateCredentialsRequest{PasswordHash: &newHash})
	require.NoError(t, err)

	creds, err := svc.GetCredentials(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.NotNil(t, creds.PasswordHash)
	assert.Equal(t, "new_hash", *creds.PasswordHash)
}

func TestUpdateCredentials_CacheInvalidationLookupFails_StillSucceeds(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccountCredentials(gomock.Any(), "cust_1", gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetAccount(gomock.Any(), "cust_1").
		Return(nil, errors.New("boom"))

	hash := "new_hash"
	err := svc.UpdateCredentials(context.Background(), "cust_1", &models.UpdateCredentialsRequest{PasswordHash: &hash})
	require.NoError(t, err)
}

func TestUpdateCredentials_MissingFields_MapsToInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		accountID string
		req       models.UpdateCredentialsRequest
	}{
		{name: "missing account_id", req: models.UpdateCredentialsRequest{}},
		{name: "no updatable field", accountID: "cust_1", req: models.UpdateCredentialsRequest{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			svc, _ := newTestService(t, ctrl)

			err := svc.UpdateCredentials(context.Background(), tc.accountID, &tc.req)
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}
