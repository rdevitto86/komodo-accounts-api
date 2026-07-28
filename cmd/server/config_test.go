package main

import (
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/idempotency"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

func setRequiredBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", ":8080")
	t.Setenv("PORT_PRIVATE", ":8081")
	t.Setenv("DYNAMODB_TABLE", "accounts")
}

func TestLoadConfigErrorsOnMissingRequiredKeys(t *testing.T) {
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected an error when required env vars are unset")
	}
}

func TestLoadConfigPopulatesRequiredFields(t *testing.T) {
	setRequiredBaseEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() returned an error: %v", err)
	}
	if cfg.publicAddr != ":8080" || cfg.privateAddr != ":8081" {
		t.Errorf("addresses = %q/%q, want :8080/:8081", cfg.publicAddr, cfg.privateAddr)
	}
	if cfg.dynamoTable != "accounts" {
		t.Errorf("dynamoTable = %q, want %q", cfg.dynamoTable, "accounts")
	}
}

func TestLoadBootstrapSecretsErrorsOnMissingRequiredKeys(t *testing.T) {
	if _, _, _, _, err := loadBootstrapSecrets(map[string]string{}); err == nil {
		t.Fatal("expected an error when required secret-backed env vars are unset")
	}
}

func TestLoadBootstrapSecretsPopulatesJWTAndUnsubscribeMaterial(t *testing.T) {
	jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, err := loadBootstrapSecrets(map[string]string{
		jwt.JWT_PUBLIC_KEY:     "pem",
		jwt.JWT_ISSUER:         "https://auth.komodo.test",
		unsubscribeTokenSecret: "unsub-key",
		"IDEMPOTENCY_TTL_SEC":  "900",
	})
	if err != nil {
		t.Fatalf("loadBootstrapSecrets() returned an error: %v", err)
	}
	if jwtPublicKey != "pem" || jwtIssuer != "https://auth.komodo.test" {
		t.Errorf("jwt material = %q/%q", jwtPublicKey, jwtIssuer)
	}
	if string(unsubscribeKey) != "unsub-key" {
		t.Errorf("unsubscribeKey = %q, want %q", unsubscribeKey, "unsub-key")
	}
	if idempotencyTTLSec != 900 {
		t.Errorf("idempotencyTTLSec = %d, want 900", idempotencyTTLSec)
	}
}

func TestLoadBootstrapSecretsAppliesIdempotencyTTLDefault(t *testing.T) {
	_, _, _, idempotencyTTLSec, err := loadBootstrapSecrets(map[string]string{
		jwt.JWT_PUBLIC_KEY:     "pem",
		jwt.JWT_ISSUER:         "https://auth.komodo.test",
		unsubscribeTokenSecret: "unsub-key",
	})
	if err != nil {
		t.Fatalf("loadBootstrapSecrets() returned an error: %v", err)
	}
	if idempotencyTTLSec != idempotency.DEFAULT_IDEM_TTL_SEC {
		t.Errorf("idempotencyTTLSec = %d, want the SDK default %d",
			idempotencyTTLSec, idempotency.DEFAULT_IDEM_TTL_SEC)
	}
}
