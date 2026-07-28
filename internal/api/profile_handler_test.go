package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/test/mocks"
)

func TestGetProfileHandler_FoundViaPathID(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccount(gomock.Any(), "account_abc").
		Return(&models.Account{AccountId: "account_abc"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/account_abc/profile", nil)
	req.SetPathValue("id", "account_abc")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetProfileHandler_NoAuth(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/me/profile", nil)
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestGetProfileHandler_NotFound(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccount(gomock.Any(), "path_account_id").
		Return(&models.Account{AccountId: "path_account_id"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/accounts/path_account_id/profile", nil)
	req.SetPathValue("id", "path_account_id")
	rr := httptest.NewRecorder()
	svc.GetProfileHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestCreateAccountHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreateAccount(gomock.Any(), "account_abc", gomock.Any()).
		Return(&models.Account{AccountId: "account_abc"}, nil)

	body := map[string]any{"email": "test@example.com", "first_name": "Test", "last_name": "Account"}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateAccountHandler_BadJSON(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateAccountHandler_NoJWT(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/me/profile", map[string]any{"email": "test@example.com"})
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestCreateAccountHandler_AlreadyExists(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreateAccount(gomock.Any(), "account_abc", gomock.Any()).
		Return(nil, fmt.Errorf("failed to create account: %w", db.ErrAlreadyExists))

	body := map[string]any{"email": "taken@example.com", "first_name": "Test", "last_name": "Account"}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestCreateAccountHandler_WritesSettingsRow(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreateAccount(gomock.Any(), "account_new", gomock.Any()).
		Return(&models.Account{AccountId: "account_new"}, nil).Times(1)

	body := map[string]any{"email": "new@example.com", "first_name": "New", "last_name": "Account"}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_new")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestCreateAccountHandler_UnknownField_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{
		"email":      "test@example.com",
		"first_name": "Test",
		"last_name":  "Account",
		"bogus":      "field",
	}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.CreateAccountHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Unit Tests: UpdateProfileHandler ─────────────────────────────────────────

func TestUpdateProfileHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccount(gomock.Any(), "account_abc", gomock.Any()).
		Return(&models.Account{AccountId: "account_abc", FirstName: "Foo"}, nil)

	req := withAccountId(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateProfileHandler_NotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccount(gomock.Any(), "account_missing", gomock.Any()).
		Return(nil, fmt.Errorf("wrapped: %w", db.ErrNotFound))

	req := withAccountId(makeRequest(t, http.MethodPut, "/v1/me/profile", map[string]any{"first_name": "Foo"}), "account_missing")
	rr := httptest.NewRecorder()
	svc.UpdateProfileHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdateProfileHandler_NoJWT(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().SoftDeleteAccount(gomock.Any(), "account_abc").Return(nil)
	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(nil, db.ErrNotFound)

	req := withAccountId(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)
}

func TestDeleteProfileHandler_NoJWT(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodDelete, "/v1/me/profile", nil)
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestDeleteProfileHandler_InvalidatesCredentialsCache(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	creds := &models.CredentialsResponse{AccountId: "account_abc", PasswordHash: ptr("hash")}
	repo.EXPECT().GetAccountCredentialsByEmail(gomock.Any(), "user@test.com").Return(creds, nil).Times(2)
	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(&models.Account{AccountId: "account_abc", Email: "user@test.com"}, nil)
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
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().SoftDeleteAccount(gomock.Any(), "account_abc").Return(errors.New("dynamodb unavailable"))

	req := withAccountId(makeRequest(t, http.MethodDelete, "/v1/me/profile", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.DeleteProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func newAvatarService(t *testing.T, ctrl *gomock.Controller, s3 *mockS3) *Service {
	t.Helper()
	repo := mocks.NewMockrepository(ctrl)
	return NewService(repo, ServiceExtraConfig{
		S3Client:     s3,
		AvatarBucket: "avatars-test",
		PresignTTL:   15 * time.Minute,
	})
}

func TestAvatarUploadHandler_PassesContentTypeAndLengthToPresign(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var gotType string
	var gotLength int64
	var gotTTL time.Duration
	s3 := &mockS3{presignPutFn: func(
		_ context.Context, bucket, key string, ttl time.Duration, contentType string, contentLength int64,
	) (string, error) {
		gotType, gotLength, gotTTL = contentType, contentLength, ttl
		assert.Equal(t, "avatars-test", bucket)
		assert.Equal(t, "avatars/account_abc/avatar", key)
		return "https://example.com/presigned-put", nil
	}}
	svc := newAvatarService(t, ctrl, s3)

	body := map[string]any{"content_type": "image/png", "content_length": 1024}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile/avatar", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.AvatarUploadHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "image/png", gotType)
	assert.Equal(t, int64(1024), gotLength)
	assert.Equal(t, 15*time.Minute, gotTTL)

	var resp models.AvatarUploadResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "https://example.com/presigned-put", resp.UploadUrl)
	assert.Equal(t, 900, resp.ExpiresInSeconds)
	assert.Equal(t, int64(MaxAvatarBytes), resp.MaxBytes)
}

func TestAvatarUploadHandler_RejectsBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "disallowed content type", body: map[string]any{"content_type": "image/gif", "content_length": 1024}},
		{name: "executable content type", body: map[string]any{"content_type": "application/octet-stream", "content_length": 1024}},
		{name: "over size cap", body: map[string]any{"content_type": "image/png", "content_length": MaxAvatarBytes + 1}},
		{name: "zero length", body: map[string]any{"content_type": "image/png", "content_length": 0}},
		{name: "negative length", body: map[string]any{"content_type": "image/png", "content_length": -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			s3 := &mockS3{presignPutFn: func(
				_ context.Context, _, _ string, _ time.Duration, _ string, _ int64,
			) (string, error) {
				t.Fatal("presign must not be called for rejected input")
				return "", nil
			}}
			svc := newAvatarService(t, ctrl, s3)

			req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile/avatar", tt.body), "account_abc")
			rr := httptest.NewRecorder()
			svc.AvatarUploadHandler(rr, req)
			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestAvatarUploadHandler_NotConfigured(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"content_type": "image/png", "content_length": 1024}
	req := withAccountId(makeRequest(t, http.MethodPost, "/v1/me/profile/avatar", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.AvatarUploadHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestAvatarUploadHandler_NoJWT(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"content_type": "image/png", "content_length": 1024}
	req := makeRequest(t, http.MethodPost, "/v1/me/profile/avatar", body)
	rr := httptest.NewRecorder()
	svc.AvatarUploadHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
