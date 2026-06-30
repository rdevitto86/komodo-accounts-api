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

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"
)

// ── Unit Tests: DeleteProfile ─────────────────────────────────────────────────

func TestDeleteProfile_GDPRDeleteUserCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetUser(gomock.Any(), "cust_1").Return(nil, db.ErrNotFound)
	repo.EXPECT().DeleteUser(gomock.Any(), "cust_1").Return(nil).Times(1)

	err := svc.DeleteProfile(context.Background(), "cust_1")
	require.NoError(t, err)
}

// ── Unit Tests: MintUnsubscribeToken / VerifyAndRecordUnsubscribe ─────────────

func TestHMACTokenRoundTrip(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetUser(gomock.Any(), "cust_1").Return(&models.User{CustomerID: "cust_1"}, nil)
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetUser(gomock.Any(), "cust_1").Return(&models.User{CustomerID: "cust_1"}, nil)

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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	key := []byte("test-secret-32-bytes-padded-xx!!")
	p := unsubPayload{
		CustomerID: "cust_1",
		Channel:    "email",
		Exp:        time.Now().Add(-1 * time.Hour).Unix(),
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

// ── Unit Tests: UpdateSettingsTags namespace ACL ──────────────────────────────

func TestUpdateSettingsTags_UnknownService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := &models.UpdateSettingsTagsRequest{Add: []string{"loyalty.vip"}}
	_, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "unknown-service", req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbiddenNamespace))
}

func TestUpdateSettingsTags_WrongPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := &models.UpdateSettingsTagsRequest{Add: []string{"system.internal"}}
	_, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "loyalty-api", req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbiddenNamespace))
}

func TestUpdateSettingsTags_Valid(t *testing.T) {
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
		Return(&models.AccountSettings{Status: "active", Tags: []string{"loyalty.vip"}}, nil)

	req := &models.UpdateSettingsTagsRequest{Add: []string{"loyalty.vip"}}
	result, err := svc.UpdateSettingsTags(context.Background(), "cust_1", "loyalty-api", req)
	require.NoError(t, err)
	assert.Contains(t, result.Tags, "loyalty.vip")
}

// ── Unit Tests: validateStatus ────────────────────────────────────────────────

func TestValidateStatus(t *testing.T) {
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
			err := validateStatus(tc.status)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
