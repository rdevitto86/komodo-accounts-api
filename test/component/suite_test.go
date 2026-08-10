package component_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/server"
	"komodo-accounts-api/test/mocks"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/ratelimit"
	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	"github.com/rdevitto86/komodo-forge-sdk-go/rules"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
	"go.uber.org/mock/gomock"
)

// ── Setup ────────────────────────────────────────────────────────────────

const (
	testIssuer          = "https://auth.komodo.test"
	testUserAudience    = "komodo-apis:user"
	testServiceAudience = "komodo-apis:service"
	testAccountID       = "acct_01H8XGJWBWBAQ4ZZ"
	testAccountEmail    = "owner@komodo.test"
	testAddressID       = "addr_01H8XGJWBWBA"
	testPaymentID       = "pay_01H8XGJWBWBAQ4"
	testCSRFToken       = "0123456789abcdef0123456789abcdef"
	testTokenTTLSeconds = 300
)

func TestMain(m *testing.M) {
	if err := os.Setenv("EVAL_RULES_PATH", "../../validation_rules.yaml"); err != nil {
		panic(err)
	}
	if !rules.LoadConfig("../../validation_rules.yaml") {
		panic("failed to load validation_rules.yaml for the component tier")
	}
	os.Exit(m.Run())
}

// ── Helpers ─────────────────────────────────────────────────────────────

type harness struct {
	publicMux  *http.ServeMux
	privateMux *http.ServeMux
	repo       *mocks.Mockrepository
	userToken  string
	svcToken   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithS3(t, nil)
}

func newHarnessWithS3(t *testing.T, s3 sdks3.API) *harness {
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

	newClient := func(audience string) *jwt.Client {
		c, err := jwt.New(t.Context(), jwt.Config{
			PublicKeyPEM:  string(pubPEM),
			PrivateKeyPEM: string(privPEM),
			Issuer:        testIssuer,
			Audience:      audience,
		})
		if err != nil {
			t.Fatalf("failed to build jwt client for audience %q: %v", audience, err)
		}
		return c
	}

	userClient := newClient(testUserAudience)
	serviceClient := newClient(testServiceAudience)

	userToken, err := userClient.SignToken(testIssuer, testAccountID, testUserAudience, testTokenTTLSeconds, nil)
	if err != nil {
		t.Fatalf("failed to sign user-plane token: %v", err)
	}
	svcToken, err := serviceClient.SignToken(
		testIssuer, testAccountID, testServiceAudience, testTokenTTLSeconds, []string{"svc:accounts"},
	)
	if err != nil {
		t.Fatalf("failed to sign service-plane token: %v", err)
	}

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockrepository(ctrl)
	svc := api.NewService(repo, api.ServiceExtraConfig{
		S3Client:       s3,
		UnsubscribeKey: []byte("test-secret-32-bytes-padded-xx!!"),
		ExportBucket:   "test-export-bucket",
		AvatarBucket:   "test-avatar-bucket",
	})

	limiter := ratelimit.New(ratelimit.Config{RPS: 10_000, Burst: 10_000})

	return &harness{
		publicMux:  server.NewPublicMux(svc, nil, userClient, "test-table", limiter),
		privateMux: server.NewPrivateMux(svc, nil, serviceClient, "test-table"),
		repo:       repo,
		userToken:  userToken,
		svcToken:   svcToken,
	}
}

func (h *harness) request(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func (h *harness) signedIn(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	req := h.request(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+h.userToken)

	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRFToken})
		req.Header.Set("X-CSRF-Token", testCSRFToken)
		req.Header.Set("Idempotency-Key", uniqueIdempotencyKey(t))
	}
	return req
}

func (h *harness) serviceCall(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	req := h.request(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+h.svcToken)
	return req
}

func uniqueIdempotencyKey(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed to generate idempotency key: %v", err)
	}
	return "ct-" + hex.EncodeToString(buf)
}

func serve(mux *http.ServeMux, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, route string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: got status %d, want %d\nbody: %s", route, rec.Code, want, rec.Body.String())
	}
}

func assertRouteRegistered(t *testing.T, mux *http.ServeMux, req *http.Request) string {
	t.Helper()
	_, pattern := mux.Handler(req)
	if pattern == "" {
		t.Fatalf("%s %s: no route is registered for this method and path", req.Method, req.URL.Path)
	}
	return pattern
}

func assertReachedHandler(t *testing.T, mux *http.ServeMux, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	pattern := assertRouteRegistered(t, mux, req)
	switch rec.Code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed:
		t.Fatalf(
			"%s: request never reached its handler, got status %d\nbody: %s",
			pattern, rec.Code, rec.Body.String(),
		)
	}
}

func assertErrorCode(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var res struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf(
			"%s %s: got status %d with a body no handler produced: %s",
			req.Method, req.URL.Path, rec.Code, rec.Body.String(),
		)
	}
	if res.Code != want {
		t.Fatalf(
			"%s %s: got status %d and error code %q, want error code %q\nbody: %s",
			req.Method, req.URL.Path, rec.Code, res.Code, want, rec.Body.String(),
		)
	}
}
