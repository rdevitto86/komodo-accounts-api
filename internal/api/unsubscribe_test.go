package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

func TestUnsubscribeHandler_ValidToken(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(&models.Account{AccountId: "cust_1"}, nil)
	token, err := svc.MintUnsubscribeToken(context.Background(), "cust_1", "email")
	require.NoError(t, err)

	repo.EXPECT().GetLatestConsent(gomock.Any(), "cust_1", "email").Return(nil, db.ErrNotFound)
	repo.EXPECT().AppendConsentLog(gomock.Any(), "cust_1", gomock.Any()).Return(nil)

	req := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": token})
	rr := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestUnsubscribeHandler_InvalidToken_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": "invalid-token"})
	rr := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUnsubscribeHandler_ExpiredToken_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	expired := buildSignedUnsubToken(t, testUnsubKey, unsubPayload{
		AccountID: "cust_1",
		Channel:   "email",
		Exp:       time.Now().Add(-1 * time.Hour).Unix(),
		JTI:       ksuid.New().String(),
	})
	req := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": expired})
	rr := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUnsubscribeHandler_ReplayProtection(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(&models.Account{AccountId: "cust_1"}, nil)
	token, err := svc.MintUnsubscribeToken(context.Background(), "cust_1", "email")
	require.NoError(t, err)

	var capturedEntry *models.ConsentLog
	repo.EXPECT().
		GetLatestConsent(gomock.Any(), "cust_1", "email").
		Return(nil, db.ErrNotFound)
	repo.EXPECT().
		AppendConsentLog(gomock.Any(), "cust_1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, e *models.ConsentLog) error {
			capturedEntry = e
			return nil
		})

	req1 := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": token})
	rr1 := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr1, req1)
	assert.Equal(t, http.StatusNoContent, rr1.Code)
	require.NotNil(t, capturedEntry)

	repo.EXPECT().
		GetLatestConsent(gomock.Any(), "cust_1", "email").
		Return(capturedEntry, nil)

	req2 := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": token})
	rr2 := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr2, req2)
	assert.Equal(t, http.StatusNoContent, rr2.Code)
}

func TestMintUnsubscribeTokenHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(&models.Account{AccountId: "cust_1"}, nil)

	body := map[string]any{"channel": "email"}
	req := makeRequest(t, http.MethodPost, "/v1/accounts/cust_1/unsubscribe-token", body)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.MintUnsubscribeTokenHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Contains(t, rr.Body.String(), "token")
}

func TestMintUnsubscribeTokenHandler_NoPathID(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"channel": "email"}
	req := makeRequest(t, http.MethodPost, "/v1/accounts//unsubscribe-token", body)
	rr := httptest.NewRecorder()
	svc.MintUnsubscribeTokenHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMintUnsubscribeTokenHandler_InvalidChannel_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"channel": "fax"}
	req := makeRequest(t, http.MethodPost, "/v1/accounts/cust_1/unsubscribe-token", body)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.MintUnsubscribeTokenHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUnsubscribeHandler_InvalidChannel_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	token := buildSignedUnsubToken(t, testUnsubKey, unsubPayload{
		AccountID: "cust_1",
		Channel:   "fax",
		Exp:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		JTI:       ksuid.New().String(),
	})
	req := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": token})
	rr := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUnsubscribeHandler_MissingJTI_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	token := buildSignedUnsubToken(t, testUnsubKey, unsubPayload{
		AccountID: "cust_1",
		Channel:   "email",
		Exp:       time.Now().Add(30 * 24 * time.Hour).Unix(),
	})
	req := makeRequest(t, http.MethodPost, "/v1/unsubscribe", map[string]any{"token": token})
	rr := httptest.NewRecorder()
	svc.UnsubscribeHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMintUnsubscribeTokenHandler_NonExistentAccount_Returns404(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "ghost_id").Return(nil, db.ErrNotFound)

	body := map[string]any{"channel": "email"}
	req := makeRequest(t, http.MethodPost, "/v1/accounts/ghost_id/unsubscribe-token", body)
	req.SetPathValue("id", "ghost_id")
	rr := httptest.NewRecorder()
	svc.MintUnsubscribeTokenHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
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

	token := buildSignedUnsubToken(t, testUnsubKey, unsubPayload{
		AccountID: "cust_1",
		Channel:   "email",
		Exp:       time.Now().Add(-1 * time.Hour).Unix(),
	})

	err := svc.VerifyAndRecordUnsubscribe(context.Background(), token, "127.0.0.1", "test-agent")
	assert.Error(t, err)
}

// ── Setup ──────────────────────────────────────────────────────────────────

var testUnsubKey = []byte("test-secret-32-bytes-padded-xx!!")

func buildSignedUnsubToken(t *testing.T, key []byte, p unsubPayload) string {
	t.Helper()
	payload, err := json.Marshal(p)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(payload, sig...))
}
