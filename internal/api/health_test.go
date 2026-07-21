package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthProbe_NoPort_Returns1(t *testing.T) {
	assert.Equal(t, 1, HealthProbe(""))
	assert.Equal(t, 1, HealthProbe("  "))
}

func TestHealthProbe_Healthy_Returns0(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	assert.Equal(t, 0, HealthProbe(probePort(t, srv.URL)))
}

func TestHealthProbe_Non2xx_Returns1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	assert.Equal(t, 1, HealthProbe(probePort(t, srv.URL)))
}

func TestHealthProbe_ConnectionRefused_Returns1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	port := probePort(t, srv.URL)
	srv.Close()

	assert.Equal(t, 1, HealthProbe(port))
}

// ── Setup ──────────────────────────────────────────────────────────────────

func probePort(t *testing.T, serverURL string) string {
	t.Helper()
	idx := strings.LastIndex(serverURL, ":")
	if idx < 0 {
		t.Fatalf("could not parse port from %q", serverURL)
	}
	return serverURL[idx+1:]
}
