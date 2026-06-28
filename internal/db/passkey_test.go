package db

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"

	"komodo-customer-api/internal/models"
)

func TestPasskeyPKSK(t *testing.T) {
	if got, want := passkeyPK("user_123"), "CUSTOMER#user_123"; got != want {
		t.Errorf("passkeyPK() = %q, want %q", got, want)
	}
	if got, want := passkeySK("cred_abc"), "PASSKEY#cred_abc"; got != want {
		t.Errorf("passkeySK() = %q, want %q", got, want)
	}
}

func TestPasskeyRecordMarshalRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	record := passkeyRecord{
		PK: passkeyPK("user_123"),
		SK: passkeySK("cred_abc"),
		PasskeyCredential: models.PasskeyCredential{
			CredentialID:   "cred_abc",
			PublicKey:      "cG9zZXVkb2tleQ==",
			SignCount:      4,
			Transports:     []string{"internal", "hybrid"},
			AAGUID:         "YWFndWlk",
			BackupEligible: true,
			BackupState:    false,
			CreatedAt:      now,
		},
	}

	av, err := attributevalue.MarshalMap(record)
	if err != nil {
		t.Fatalf("MarshalMap() error = %v", err)
	}

	var got passkeyRecord
	if err := attributevalue.UnmarshalMap(av, &got); err != nil {
		t.Fatalf("UnmarshalMap() error = %v", err)
	}

	if got.PK != record.PK || got.SK != record.SK {
		t.Errorf("PK/SK = %q/%q, want %q/%q", got.PK, got.SK, record.PK, record.SK)
	}
	if got.CredentialID != record.CredentialID {
		t.Errorf("CredentialID = %q, want %q", got.CredentialID, record.CredentialID)
	}
	if got.SignCount != record.SignCount {
		t.Errorf("SignCount = %d, want %d", got.SignCount, record.SignCount)
	}
	if len(got.Transports) != 2 || got.Transports[0] != "internal" || got.Transports[1] != "hybrid" {
		t.Errorf("Transports = %v, want %v", got.Transports, record.Transports)
	}
	if !got.CreatedAt.Equal(record.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, record.CreatedAt)
	}
	if got.LastUsedAt != nil {
		t.Errorf("LastUsedAt = %v, want nil", got.LastUsedAt)
	}
}

func TestCreatePasskey_RequiresCredentialID(t *testing.T) {
	cred := &models.PasskeyCredential{PublicKey: "cG9zZXVkb2tleQ=="}
	r := &Repo{}
	if err := r.CreatePasskey(context.Background(), "user_123", cred); err == nil {
		t.Fatal("expected error for missing credential_id, got nil")
	}
}
