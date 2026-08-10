package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/redaction"
)

func TestScopeObservabilityMiddleware_PassesThroughUntouched(t *testing.T) {
	t.Parallel()

	calls := 0
	var seen []string

	next := http.HandlerFunc(func(wtr http.ResponseWriter, req *http.Request) {
		calls++
		seen = ctxKeys.GetScopes(req.Context())
		wtr.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/credentials", http.NoBody)
	req = req.WithContext(context.WithValue(
		req.Context(), ctxKeys.SCOPES_KEY, []string{"svc:accounts", "svc:billing"},
	))
	rec := httptest.NewRecorder()

	JWTScopeObservabilityMiddleware(next).ServeHTTP(rec, req)

	if calls != 1 {
		t.Fatalf("next handler ran %d times, want 1", calls)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusTeapot)
	}
	if !slices.Equal(seen, []string{"svc:accounts", "svc:billing"}) {
		t.Fatalf("next handler saw scopes %v, want the scopes set upstream", seen)
	}
}

func TestScopeObservabilityMiddleware_DoesNotRejectMissingScopes(t *testing.T) {
	t.Parallel()

	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/credentials", http.NoBody)
	rec := httptest.NewRecorder()

	JWTScopeObservabilityMiddleware(next).ServeHTTP(rec, req)

	if !reached {
		t.Fatal("next handler was not reached for a request carrying no scopes")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("middleware wrote a response body: %s", rec.Body.String())
	}
}

func TestScopeObservabilityMiddleware_ScopesSurviveStrictRedaction(t *testing.T) {
	t.Parallel()

	scopes := []string{"svc:accounts", "svc:accounts:credentials.write", "read:accounts"}

	got, ok := redaction.RedactValue(scopes).([]string)
	if !ok {
		t.Fatalf("strict redaction returned %T, want []string", redaction.RedactValue(scopes))
	}
	if !slices.Equal(got, scopes) {
		t.Fatalf("strict redaction rewrote scopes to %v, want %v", got, scopes)
	}
}
