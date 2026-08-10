package component_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/internal/server"
	"komodo-accounts-api/test/mocks"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

// ── Setup ────────────────────────────────────────────────────────────────

const testPasskeyCredentialID = "pk_01H8XGJWBWBAQ4"

var privateScopeRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/v1/accounts/credentials"},
	{http.MethodGet, "/v1/accounts/" + testAccountID},
	{http.MethodGet, "/v1/accounts/" + testAccountID + "/addresses"},
	{http.MethodGet, "/v1/accounts/" + testAccountID + "/preferences"},
	{http.MethodGet, "/v1/accounts/" + testAccountID + "/payments"},
	{http.MethodPut, "/v1/accounts/" + testAccountID + "/credentials"},
	{http.MethodGet, "/v1/accounts/" + testAccountID + "/passkeys"},
	{http.MethodPost, "/v1/accounts/" + testAccountID + "/passkeys"},
	{http.MethodPatch, "/v1/accounts/" + testAccountID + "/passkeys/" + testPasskeyCredentialID},
	{http.MethodDelete, "/v1/accounts/" + testAccountID + "/passkeys/" + testPasskeyCredentialID},
	{http.MethodGet, "/v1/accounts/" + testAccountID + "/settings"},
	{http.MethodPut, "/v1/accounts/" + testAccountID + "/settings"},
	{http.MethodPut, "/v1/accounts/" + testAccountID + "/settings/tags"},
	{http.MethodPost, "/v1/accounts/" + testAccountID + "/communications/unsubscribe-token"},
}

// ── Helpers ─────────────────────────────────────────────────────────────

type scopeHarness struct {
	privateMux *http.ServeMux
	repo       *mocks.Mockrepository
	sign       func(t *testing.T, scopes []string) string
}

func newScopeHarness(t *testing.T) *scopeHarness {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	client, err := jwt.New(t.Context(), jwt.Config{
		PublicKeyPEM:  string(pubPEM),
		PrivateKeyPEM: string(privPEM),
		Issuer:        testIssuer,
		Audience:      testServiceAudience,
	})
	if err != nil {
		t.Fatalf("failed to build jwt client for audience %q: %v", testServiceAudience, err)
	}

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockrepository(ctrl)
	svc := api.NewService(repo, api.ServiceExtraConfig{
		UnsubscribeKey: []byte("test-secret-32-bytes-padded-xx!!"),
		ExportBucket:   "test-export-bucket",
		AvatarBucket:   "test-avatar-bucket",
	})

	return &scopeHarness{
		privateMux: server.NewPrivateMux(svc, nil, client, "test-table"),
		repo:       repo,
		sign: func(t *testing.T, scopes []string) string {
			t.Helper()
			token, err := client.SignToken(
				testIssuer, testAccountID, testServiceAudience, testTokenTTLSeconds, scopes,
			)
			if err != nil {
				t.Fatalf("failed to sign service-plane token with scopes %v: %v", scopes, err)
			}
			return token
		},
	}
}

func (h *scopeHarness) call(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, http.NoBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	assertRouteRegistered(t, h.privateMux, req)

	return serve(h.privateMux, req)
}

func (h *scopeHarness) allowEveryPrivateRepoCall() {
	h.repo.EXPECT().DeletePasskey(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
	h.repo.EXPECT().GetAccount(gomock.Any(), gomock.Any()).
		Return(&models.Account{AccountId: testAccountID}, nil).AnyTimes()
	h.repo.EXPECT().GetAccountAddresses(gomock.Any(), gomock.Any()).
		Return([]models.Address{}, nil).AnyTimes()
	h.repo.EXPECT().GetAccountPreferences(gomock.Any(), gomock.Any()).
		Return(&models.Preferences{}, nil).AnyTimes()
	h.repo.EXPECT().ListPayments(gomock.Any(), gomock.Any()).
		Return([]models.PaymentMethod{}, nil).AnyTimes()
	h.repo.EXPECT().GetAccountPasskeys(gomock.Any(), gomock.Any()).
		Return([]models.PasskeyCredential{}, nil).AnyTimes()
	h.repo.EXPECT().GetSettings(gomock.Any(), gomock.Any()).
		Return(&models.AccountSettings{}, nil).AnyTimes()
	h.repo.EXPECT().GetAccountCredentialsByEmail(gomock.Any(), gomock.Any()).
		Return(&models.CredentialsResponse{}, nil).AnyTimes()
}

func assertNotBlockedByAuth(t *testing.T, rec *httptest.ResponseRecorder, method, path string) {
	t.Helper()
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf(
			"%s %s: service-plane caller was blocked with status %d\nbody: %s",
			method, path, rec.Code, rec.Body.String(),
		)
	}
}

func TestComponent_PrivateMux_ServiceScopedCallerPassesEveryPrivateRoute(t *testing.T) {
	testutil.Component(t)

	h := newScopeHarness(t)
	h.allowEveryPrivateRepoCall()
	token := h.sign(t, []string{"svc:accounts"})

	for _, route := range privateScopeRoutes {
		rec := h.call(t, route.method, route.path, token)
		assertNotBlockedByAuth(t, rec, route.method, route.path)
	}
}

func TestComponent_PrivateMux_NonServiceScopeRejectedOnEveryPrivateRoute(t *testing.T) {
	testutil.Component(t)

	h := newScopeHarness(t)
	token := h.sign(t, []string{"read:accounts", "write:accounts"})

	for _, route := range privateScopeRoutes {
		req := httptest.NewRequest(route.method, route.path, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		assertRouteRegistered(t, h.privateMux, req)

		rec := serve(h.privateMux, req)
		assertStatus(t, rec, http.StatusForbidden, route.method+" "+route.path)
		assertErrorCode(t, req, rec, httpErr.Auth.InsufficientScope.ID)
	}
}

func TestComponent_PrivateMux_ScopelessTokenRejectedOnEveryPrivateRoute(t *testing.T) {
	testutil.Component(t)

	h := newScopeHarness(t)
	token := h.sign(t, nil)

	for _, route := range privateScopeRoutes {
		req := httptest.NewRequest(route.method, route.path, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		assertRouteRegistered(t, h.privateMux, req)

		rec := serve(h.privateMux, req)
		assertStatus(t, rec, http.StatusForbidden, route.method+" "+route.path)
		assertErrorCode(t, req, rec, httpErr.Auth.InsufficientScope.ID)
	}
}

func TestComponent_PrivateMux_MissingTokenRejectedOnEveryPrivateRoute(t *testing.T) {
	testutil.Component(t)

	h := newScopeHarness(t)

	for _, route := range privateScopeRoutes {
		req := httptest.NewRequest(route.method, route.path, http.NoBody)
		assertRouteRegistered(t, h.privateMux, req)

		rec := serve(h.privateMux, req)
		assertStatus(t, rec, http.StatusUnauthorized, route.method+" "+route.path)
		assertErrorCode(t, req, rec, httpErr.Auth.InvalidToken.ID)
	}
}
