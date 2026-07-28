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
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	account := &models.CreateAccountRequest{
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "Account",
	}

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	_, err := repo.CreateAccount(context.Background(), "cust_123", account)
	require.NoError(t, err)
	require.Len(t, captured, 3)

	for i, item := range captured {
		assert.Equal(t, "test-table", item.Table, "item %d", i)
		assert.Equal(t, dynamodb.TransactPut, item.Op, "item %d", i)
	}

	profileRec, ok := captured[0].Item.(accountRecord)
	require.True(t, ok)
	assert.Equal(t, "PROFILE", profileRec.SK)

	settingsRec, ok := captured[1].Item.(settingsDefaultRecord)
	require.True(t, ok)
	assert.Equal(t, "SETTINGS", settingsRec.SK)

	emailRec, ok := captured[emailReservationTransactIndex].Item.(emailReservationRecord)
	require.True(t, ok)
	assert.Equal(t, emailReservationSK, emailRec.SK)
}

func TestCreateAccount_ConditionalCheckFailure_MapsToAlreadyExists(t *testing.T) {
	t.Parallel()
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

	_, err := repo.CreateAccount(context.Background(), "cust_123", &models.CreateAccountRequest{
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestCreateAccount_NonConditionalCancellation_PropagatesAsRetryable(t *testing.T) {
	t.Parallel()
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

	_, err := repo.CreateAccount(context.Background(), "cust_123", &models.CreateAccountRequest{
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrAlreadyExists))
}

func TestUpdateAccount_PartialUpdate_ExcludesPasswordHash(t *testing.T) {
	t.Parallel()
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
	updated, err := repo.UpdateAccount(context.Background(), "cust_1", &models.UpdateAccountRequest{
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
	t.Parallel()
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
	_, err := repo.UpdateAccount(context.Background(), "cust_1", &models.UpdateAccountRequest{FirstName: &firstName})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetAccountCredentialsByEmail_Success(t *testing.T) {
	t.Parallel()
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
				{SK: "SETTINGS", EmailVerified: true, Status: "suspended"},
			}
			return nil
		})

	creds, err := repo.GetAccountCredentialsByEmail(context.Background(), "test@example.com")
	require.NoError(t, err)
	assert.Equal(t, "cust_1", creds.AccountId)
	require.NotNil(t, creds.PasswordHash)
	assert.Equal(t, "hashed", *creds.PasswordHash)
	assert.True(t, creds.EmailVerified)
	assert.Equal(t, []models.CredentialsResponseAuthMethods{"password"}, creds.AuthMethods)
	assert.Equal(t, models.CredentialsResponseStatus("suspended"), creds.Status)
}

func TestGetAccountCredentialsByEmail_ProfileMissing_MapsToNotFound(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestGetAccountCredentialsByEmail_MissingSettings_DefaultsStatusToActive(t *testing.T) {
	t.Parallel()
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
				{SK: "PROFILE", AccountID: "cust_1", PasswordHash: "hashed"},
			}
			return nil
		})

	creds, err := repo.GetAccountCredentialsByEmail(context.Background(), "test@example.com")
	require.NoError(t, err)
	assert.Equal(t, models.CredentialsResponseStatus("active"), creds.Status)
	assert.False(t, creds.EmailVerified)
}

func TestGetAccountCredentialsByEmail_BatchError_IsNotMappedToNotFound(t *testing.T) {
	t.Parallel()
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
		Return(errors.New("unprocessed read keys remain after 5 retries"))

	_, err := repo.GetAccountCredentialsByEmail(context.Background(), "test@example.com")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound),
		"a throttled/exhausted batch read must surface as a retryable 5xx, never as a 404 that "+
			"tells auth-api the account does not exist")
}

func TestCreateAccount_DuplicateEmail_MapsToEmailAlreadyExists(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("None")},
				{Code: aws.String("None")},
				{Code: aws.String("ConditionalCheckFailed")},
			},
		})

	_, err := repo.CreateAccount(context.Background(), "cust_2", &models.CreateAccountRequest{
		Email:     "taken@example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmailAlreadyExists))
	assert.False(t, errors.Is(err, ErrAlreadyExists),
		"a duplicate email must be distinguishable from a duplicate account_id")
}

func TestCreateAccount_WritesEmailReservation(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	_, err := repo.CreateAccount(context.Background(), "cust_1", &models.CreateAccountRequest{
		Email:     "Mixed@Example.com",
		FirstName: "Test",
		LastName:  "Account",
	})
	require.NoError(t, err)
	require.Len(t, captured, 3)

	rec, ok := captured[emailReservationTransactIndex].Item.(emailReservationRecord)
	require.True(t, ok, "the third transaction item must be the email reservation")
	assert.Equal(t, "EMAIL#mixed@example.com", rec.PK, "reservation key must be lowercased")
	assert.Equal(t, emailReservationSK, rec.SK)
	assert.Equal(t, "cust_1", rec.AccountID)
}

func TestDeleteAccount_ReleasesEmailReservationWithProfile(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAll(gomock.Any(), gomock.Any()).
		Return([]map[string]ddbTypes.AttributeValue{
			{
				"PK":    &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#cust_1"},
				"SK":    &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
				"email": &ddbTypes.AttributeValueMemberS{Value: "gone@example.com"},
			},
			{
				"PK": &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#cust_1"},
				"SK": &ddbTypes.AttributeValueMemberS{Value: "SETTINGS"},
			},
		}, nil)

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	require.NoError(t, repo.DeleteAccount(context.Background(), "cust_1"))
	require.Len(t, captured, 3)

	var foundReservation bool
	for _, item := range captured {
		if pk, ok := item.Key["PK"].(*ddbTypes.AttributeValueMemberS); ok &&
			pk.Value == "EMAIL#gone@example.com" {
			foundReservation = true
		}
	}
	assert.True(t, foundReservation, "hard erase must release the email reservation")
	assert.Equal(t, "PROFILE", captured[0].Key["SK"].(*ddbTypes.AttributeValueMemberS).Value,
		"profile and its reservation must lead the batch so they share one transaction")
}

func TestResolveAccountIDByEmail_Duplicates_RefusesAmbiguousLookup(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAllAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) error {
			results, ok := out.(*[]gsiEmailResult)
			require.True(t, ok)
			*results = []gsiEmailResult{
				{AccountID: "cust_1", PK: "ACCOUNT#cust_1"},
				{AccountID: "cust_2", PK: "ACCOUNT#cust_2"},
			}
			return nil
		})

	_, err := repo.GetAccountCredentialsByEmail(context.Background(), "dupe@example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmailAlreadyExists))
	assert.False(t, errors.Is(err, ErrNotFound),
		"an ambiguous email must never resolve to a login for an arbitrarily chosen account")
}
