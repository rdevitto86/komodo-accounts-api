package main

import (
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/idempotency"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

func TestSecretKeysExcludeCDKInjectedNames(t *testing.T) {
	cdkInjected := map[string]bool{
		"DYNAMODB_TABLE": true,
		"PORT":           true,
		"PORT_PRIVATE":   true,
		"AWS_REGION":     true,
	}
	for _, key := range secretKeys {
		if cdkInjected[key] {
			t.Errorf("%q is injected by CDK and must not also be fetched as a secret", key)
		}
	}
}

func TestSecretKeysExcludeSingleJWTAudience(t *testing.T) {
	for _, key := range secretKeys {
		if key == "JWT_AUDIENCE" {
			t.Error("JWT_AUDIENCE must not be fetched: this service verifies two token planes " +
				"with separate audiences, configured via JWT_AUDIENCE_USER and JWT_AUDIENCE_SERVICE")
		}
	}
}

func TestMissingKeys_AllPresent_ReturnsEmpty(t *testing.T) {
	missing := missingKeys(
		envVar{"PORT", ":8080"},
		envVar{"PORT_PRIVATE", ":8081"},
	)
	if len(missing) != 0 {
		t.Errorf("missingKeys() = %v, want empty", missing)
	}
}

func TestMissingKeys_AllEmpty_ReturnsAllKeys(t *testing.T) {
	missing := missingKeys(
		envVar{"PORT", ""},
		envVar{"PORT_PRIVATE", ""},
	)
	want := []string{"PORT", "PORT_PRIVATE"}
	if len(missing) != len(want) {
		t.Fatalf("missingKeys() = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Errorf("missingKeys()[%d] = %q, want %q", i, missing[i], want[i])
		}
	}
}

func TestMissingKeys_MixedPresenceAndEmpty_PreservesInputOrder(t *testing.T) {
	missing := missingKeys(
		envVar{"PORT", ":8080"},
		envVar{"PORT_PRIVATE", ""},
		envVar{"DYNAMODB_TABLE", ""},
		envVar{"S3_EXPORT_BUCKET", "bucket"},
		envVar{"S3_AVATAR_BUCKET", ""},
	)
	want := []string{"PORT_PRIVATE", "DYNAMODB_TABLE", "S3_AVATAR_BUCKET"}
	if len(missing) != len(want) {
		t.Fatalf("missingKeys() = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Errorf("missingKeys()[%d] = %q, want %q", i, missing[i], want[i])
		}
	}
}

func TestMissingKeys_NoVars_ReturnsNil(t *testing.T) {
	if missing := missingKeys(); missing != nil {
		t.Errorf("missingKeys() = %v, want nil", missing)
	}
}

func TestLoadBootstrapSecretsErrorsOnMissingRequiredKeys(t *testing.T) {
	if _, _, _, _, _, err := setBootstrapSecrets(map[string]string{}); err == nil {
		t.Fatal("expected an error when required secret-backed env vars are unset")
	}
}

func TestLoadBootstrapSecretsPopulatesJWTAndUnsubscribeMaterial(t *testing.T) {
	jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, _, err := setBootstrapSecrets(map[string]string{
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
	_, _, _, idempotencyTTLSec, _, err := setBootstrapSecrets(map[string]string{
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
