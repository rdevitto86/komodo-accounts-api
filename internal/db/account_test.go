package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"

	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/test/mocks"
)

func TestCreateAccount_TransactWritePayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	now := time.Now().UTC()
	account := &models.Account{
		AccountID:   "cust_123",
		Email:       "test@example.com",
		FirstName:   "Test",
		LastName:    "Account",
		AuthMethods: []string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	err := repo.CreateAccount(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, captured, 2)

	assert.Equal(t, "test-table", captured[0].Table)
	assert.Equal(t, dynamodb.TransactPut, captured[0].Op)
	assert.Equal(t, "test-table", captured[1].Table)
	assert.Equal(t, dynamodb.TransactPut, captured[1].Op)

	profileRec, ok := captured[0].Item.(accountRecord)
	require.True(t, ok)
	assert.Equal(t, "PROFILE", profileRec.SK)

	settingsRec, ok := captured[1].Item.(settingsDefaultRecord)
	require.True(t, ok)
	assert.Equal(t, "SETTINGS", settingsRec.SK)
}

func TestCreateAccount_ConditionalCheckFailure_MapsToAlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("None")},
				{Code: aws.String("ConditionalCheckFailed")},
			},
		})

	err := repo.CreateAccount(context.Background(), &models.Account{
		AccountID: "cust_123",
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestCreateAccount_NonConditionalCancellation_PropagatesAsRetryable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("None")},
				{Code: aws.String("ThrottlingError")},
			},
		})

	err := repo.CreateAccount(context.Background(), &models.Account{
		AccountID: "cust_123",
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrAlreadyExists))
}

func TestUpdateAccount_PartialUpdate_ExcludesPasswordHash(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	key := map[string]ddbTypes.AttributeValue{
		"PK": &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#cust_1"},
		"SK": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
	}
	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "PROFILE").Return(key, nil)

	var capturedNames map[string]string
	client.EXPECT().
		UpdateItemAs(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ map[string]ddbTypes.AttributeValue, _ string, _ map[string]ddbTypes.AttributeValue, names map[string]string, cond *string, out any) error {
			capturedNames = names
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_exists(SK)", *cond)
			rec, ok := out.(*accountRecord)
			require.True(t, ok)
			rec.AccountID = "cust_1"
			rec.FirstName = "Updated"
			return nil
		})

	phone := "555-1234"
	firstName := "Updated"
	updated, err := repo.UpdateAccount(context.Background(), "cust_1", &models.UpdateProfileRequest{
		Phone:     &phone,
		FirstName: &firstName,
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.FirstName)

	touched := make(map[string]bool, len(capturedNames))
	for _, attr := range capturedNames {
		touched[attr] = true
	}
	assert.True(t, touched["phone"])
	assert.True(t, touched["first_name"])
	assert.True(t, touched["updated_at"])
	assert.False(t, touched["last_name"])
	assert.False(t, touched["avatar_url"])
	assert.False(t, touched["password_hash"])
	assert.False(t, touched["auth_methods"])
}

func TestUpdateAccount_ConditionFailure_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	key := map[string]ddbTypes.AttributeValue{
		"PK": &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#cust_1"},
		"SK": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
	}
	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "PROFILE").Return(key, nil)
	client.EXPECT().
		UpdateItemAs(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(conditionalCheckErr())

	firstName := "Updated"
	_, err := repo.UpdateAccount(context.Background(), "cust_1", &models.UpdateProfileRequest{FirstName: &firstName})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetAccountCredentialsByEmail_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAllAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) error {
			results, ok := out.(*[]gsiEmailResult)
			require.True(t, ok)
			*results = []gsiEmailResult{{AccountID: "cust_1", PK: "ACCOUNT#cust_1"}}
			return nil
		})
	client.EXPECT().
		BatchGetItemAs(gomock.Any(), "test-table", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ []map[string]ddbTypes.AttributeValue, out any) error {
			items, ok := out.(*[]credentialsBatchItem)
			require.True(t, ok)
			*items = []credentialsBatchItem{
				{SK: "PROFILE", AccountID: "cust_1", PasswordHash: "hashed", AuthMethods: []string{"password"}},
				{SK: "SETTINGS", EmailVerified: true},
			}
			return nil
		})

	creds, err := repo.GetAccountCredentialsByEmail(context.Background(), "test@example.com")
	require.NoError(t, err)
	assert.Equal(t, "cust_1", creds.AccountID)
	assert.Equal(t, "hashed", creds.PasswordHash)
	assert.True(t, creds.EmailVerified)
	assert.Equal(t, []string{"password"}, creds.AuthMethods)
}

func TestGetAccountCredentialsByEmail_ProfileMissing_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAllAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) error {
			results, ok := out.(*[]gsiEmailResult)
			require.True(t, ok)
			*results = []gsiEmailResult{{AccountID: "cust_1", PK: "ACCOUNT#cust_1"}}
			return nil
		})
	client.EXPECT().
		BatchGetItemAs(gomock.Any(), "test-table", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ []map[string]ddbTypes.AttributeValue, out any) error {
			items, ok := out.(*[]credentialsBatchItem)
			require.True(t, ok)
			*items = []credentialsBatchItem{{SK: "SETTINGS", EmailVerified: true}}
			return nil
		})

	_, err := repo.GetAccountCredentialsByEmail(context.Background(), "test@example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestConsentSK_FixedWidthOrdering(t *testing.T) {
	exactSecond := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	withFraction := exactSecond.Add(500 * time.Millisecond)

	skExactSecond := consentSK("email", exactSecond)
	skWithFraction := consentSK("email", withFraction)

	assert.Less(t, skExactSecond, skWithFraction,
		"exact-second SK must sort before a later SK with a fractional component")
}

// ── Setup ──────────────────────────────────────────────────────────────────

func conditionalCheckErr() error {
	return fmt.Errorf("failed conditional check: %w", &ddbTypes.ConditionalCheckFailedException{})
}
