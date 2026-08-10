package db

import (
	"context"
	"errors"
	"fmt"
	"komodo-accounts-api/internal/models"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
)

type passkeyRecord struct {
	PK             string     `dynamodbav:"PK"`
	SK             string     `dynamodbav:"SK"`
	CredentialID   string     `dynamodbav:"credential_id"`
	PublicKey      string     `dynamodbav:"public_key"`
	SignCount      int64      `dynamodbav:"sign_count"`
	Transports     []string   `dynamodbav:"transports,omitempty"`
	AAGUID         string     `dynamodbav:"aaguid,omitempty"`
	BackupEligible bool       `dynamodbav:"backup_eligible"`
	BackupState    bool       `dynamodbav:"backup_state"`
	CreatedAt      time.Time  `dynamodbav:"created_at"`
	LastUsedAt     *time.Time `dynamodbav:"last_used_at,omitempty"`
}

// DB method that creates a passkey credential for an account.
func (r *Repo) CreatePasskey(ctx context.Context, accountID string, cred *models.PasskeyCredential) error {
	if cred.CredentialId == "" {
		return fmt.Errorf("credential_id is required")
	}

	now := time.Now().UTC()
	cred.CreatedAt = now
	cred.LastUsedAt = nil

	record := passkeyRecordFrom(createPK(accountID), passkeySK(cred.CredentialId), cred)

	condition := "attribute_not_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("failed to create passkey: %w", ErrPasskeyAlreadyExists)
		}
		return fmt.Errorf("failed to write passkey: %w", err)
	}
	return nil
}

// DB method that retrieves all passkey credentials for an account.
func (r *Repo) GetAccountPasskeys(ctx context.Context, accountID string) ([]models.PasskeyCredential, error) {
	var records []passkeyRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PASSKEY#"},
		},
		Limit: aws.Int32(100),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to query passkeys: %w", err)
	}

	creds := make([]models.PasskeyCredential, len(records))
	for i := range records {
		creds[i] = records[i].toModel()
	}
	return creds, nil
}

// DB method that updates a passkey credential for an account.
func (r *Repo) UpdatePasskey(ctx context.Context, accountID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error) {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", passkeySK(credentialID))
	if err != nil {
		return nil, fmt.Errorf("failed to build passkey key: %w", err)
	}

	setClauses := []string{"#sc = :new", "#bs = :bs"}
	exprValues := map[string]ddbTypes.AttributeValue{
		":new": &ddbTypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", update.SignCount)},
		":bs":  &ddbTypes.AttributeValueMemberBOOL{Value: update.BackupState},
	}
	exprNames := map[string]string{
		"#sc": "sign_count",
		"#bs": "backup_state",
	}
	if update.LastUsedAt != nil {
		setClauses = append(setClauses, "#lua = :lua")
		exprValues[":lua"] = &ddbTypes.AttributeValueMemberS{Value: update.LastUsedAt.UTC().Format(time.RFC3339Nano)}
		exprNames["#lua"] = "last_used_at"
	}

	updateExpr := "SET " + strings.Join(setClauses, ", ")
	condition := "attribute_exists(SK) AND #sc <= :new"

	var updated passkeyRecord
	if err := r.client.UpdateItemAs(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition, &updated); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			var check passkeyRecord
			if getErr := r.client.GetItemAs(ctx, r.table, key, false, nil, &check); getErr != nil {
				return nil, fmt.Errorf("passkey not found: %w", ErrNotFound)
			}
			return nil, fmt.Errorf("passkey sign count regression rejected: %w", ErrPasskeySignCountRegression)
		}
		return nil, fmt.Errorf("failed to update passkey: %w", err)
	}
	cred := updated.toModel()
	return &cred, nil
}

// DB method that deletes a passkey credential for an account.
func (r *Repo) DeletePasskey(ctx context.Context, accountID, credentialID string) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", passkeySK(credentialID))
	if err != nil {
		return fmt.Errorf("failed to build passkey key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}

// Helper that converts a passkeyRecord model to a PasskeyCredential model.
func (r *passkeyRecord) toModel() models.PasskeyCredential {
	return models.PasskeyCredential{
		CredentialId:   r.CredentialID,
		PublicKey:      r.PublicKey,
		SignCount:      r.SignCount,
		Transports:     ptrSlice(r.Transports),
		Aaguid:         optStr(r.AAGUID),
		BackupEligible: r.BackupEligible,
		BackupState:    r.BackupState,
		CreatedAt:      r.CreatedAt,
		LastUsedAt:     r.LastUsedAt,
	}
}

// Helper that converts a PasskeyCredential model to a local passkeyRecord model.
func passkeyRecordFrom(pk, sk string, c *models.PasskeyCredential) passkeyRecord {
	return passkeyRecord{
		PK:             pk,
		SK:             sk,
		CredentialID:   c.CredentialId,
		PublicKey:      c.PublicKey,
		SignCount:      c.SignCount,
		Transports:     derefSlice(c.Transports),
		AAGUID:         deref(c.Aaguid),
		BackupEligible: c.BackupEligible,
		BackupState:    c.BackupState,
		CreatedAt:      c.CreatedAt,
		LastUsedAt:     c.LastUsedAt,
	}
}

func passkeySK(credentialID string) string { return "PASSKEY#" + credentialID }
