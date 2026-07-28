package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

func TestDeleteProfile_GDPRDeleteAccountCalled(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(nil, db.ErrNotFound)
	repo.EXPECT().DeleteAccount(gomock.Any(), "cust_1").Return(nil).Times(1)

	err := svc.DeleteProfile(context.Background(), "cust_1")
	require.NoError(t, err)
}

func TestHMACTokenRoundTrip(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(&models.Account{AccountId: "cust_1"}, nil)
	repo.EXPECT().GetLatestConsent(gomock.Any(), "cust_1", "email").Return(nil, db.ErrNotFound)
	repo.EXPECT().
		AppendConsentLog(gomock.Any(), "cust_1", gomock.Any()).
		Return(nil)

	token, err := svc.MintUnsubscribeToken(context.Background(), "cust_1", "email")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	err = svc.VerifyAndRecordUnsubscribe(context.Background(), token, "127.0.0.1", "test-agent")
	require.NoError(t, err)
}

func TestHMACTokenTampered(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(&models.Account{AccountId: "cust_1"}, nil)

	token, err := svc.MintUnsubscribeToken(context.Background(), "cust_1", "email")
	require.NoError(t, err)

	raw, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)

	raw[len(raw)-1] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	err = svc.VerifyAndRecordUnsubscribe(context.Background(), tampered, "127.0.0.1", "test-agent")
	assert.Error(t, err)
}

func TestHMACTokenExpired(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	key := []byte("test-secret-32-bytes-padded-xx!!")
	p := unsubPayload{
		AccountID: "cust_1",
		Channel:   "email",
		Exp:       time.Now().Add(-1 * time.Hour).Unix(),
	}
	payload, err := json.Marshal(p)
	require.NoError(t, err)

	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(append(payload, sig...))

	err = svc.VerifyAndRecordUnsubscribe(context.Background(), token, "127.0.0.1", "test-agent")
	assert.Error(t, err)
}

func TestUpdateProfile_OnlyTranslatesSuppliedFields(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	var captured *models.UpdateAccountRequest
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, req *models.UpdateAccountRequest) (*models.Account, error) {
			captured = req
			return &models.Account{AccountId: "cust_1"}, nil
		})

	firstName := "Jane"
	_, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		FirstName: &firstName,
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.FirstName)
	assert.Equal(t, "Jane", *captured.FirstName)
	assert.Nil(t, captured.Phone)
	assert.Nil(t, captured.LastName)
	assert.Nil(t, captured.AvatarUrl)
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

func TestAddAddress_AlreadyExists(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreateAddress(gomock.Any(), "cust_1", gomock.Any()).
		Return(db.ErrAlreadyExists)

	err := svc.AddAddress(context.Background(), "cust_1", &models.Address{AddressId: "addr_dupe"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestUpsertPayment_AlreadyExists(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpsertPayment(gomock.Any(), "cust_1", gomock.Any()).
		Return(db.ErrAlreadyExists)

	err := svc.UpsertPayment(context.Background(), "cust_1", &models.PaymentMethod{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestUpsertPayment_NotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpsertPayment(gomock.Any(), "cust_1", gomock.Any()).
		Return(db.ErrNotFound)

	err := svc.UpsertPayment(context.Background(), "cust_1", &models.PaymentMethod{PaymentId: "pay_foreign"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestUpdateSettingsTags_UnknownService(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := &models.UpdateSettingsTagsRequest{Add: ptr([]string{"loyalty.vip"})}
	_, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "unknown-service", req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbiddenNamespace))
}

func TestUpdateSettingsTags_WrongPrefix(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := &models.UpdateSettingsTagsRequest{Add: ptr([]string{"system.internal"})}
	_, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "loyalty-api", req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbiddenNamespace))
}

func TestUpdateSettingsTags_Valid(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetSettings(gomock.Any(), "cust_1").Return(nil, db.ErrNotFound)
	repo.EXPECT().UpdateSettings(gomock.Any(), "cust_1", gomock.Any()).Return(nil)
	repo.EXPECT().
		MutateSettingsTags(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active", Tags: ptr([]string{"loyalty.vip"})}, nil)

	req := &models.UpdateSettingsTagsRequest{Add: ptr([]string{"loyalty.vip"})}
	result, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "loyalty-api", req)
	require.NoError(t, err)
	assert.Contains(t, deref(result.Tags), "loyalty.vip")
}

func TestUpdateSettingsTags_MissingSettingsRecord_MapsToNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active"}, nil)
	repo.EXPECT().
		MutateSettingsTags(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(db.ErrNotFound)

	req := &models.UpdateSettingsTagsRequest{Add: ptr([]string{"loyalty.vip"})}
	_, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "loyalty-api", req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestValidateStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status  string
		wantErr bool
	}{
		{"active", false},
		{"suspended", false},
		{"closed", false},
		{"pending_deletion", false},
		{"invalid", true},
		{"", true},
		{"ACTIVE", true},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			err := validateStatus(tc.status)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateAccount_MissingFields_MapsToInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		accountID string
		req       models.CreateAccountRequest
	}{
		{name: "missing account_id", req: models.CreateAccountRequest{Email: "a@b.co", FirstName: "A", LastName: "B"}},
		{name: "missing email", accountID: "cust_1", req: models.CreateAccountRequest{FirstName: "A", LastName: "B"}},
		{name: "missing first_name", accountID: "cust_1", req: models.CreateAccountRequest{Email: "a@b.co", LastName: "B"}},
		{name: "missing last_name", accountID: "cust_1", req: models.CreateAccountRequest{Email: "a@b.co", FirstName: "A"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			svc, _ := newTestService(t, ctrl)

			_, err := svc.CreateAccount(context.Background(), tc.accountID, &tc.req)
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
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
