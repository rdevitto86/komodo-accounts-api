package db

import (
	"context"
	"testing"
	"time"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komodo-customer-api/internal/models"
)

// ── Unit Tests ───────────────────────────────────────────────────────────────

type mockTxClient struct {
	calls []*awsdynamodb.TransactWriteItemsInput
	err   error
}

func (m *mockTxClient) TransactWriteItems(ctx context.Context, params *awsdynamodb.TransactWriteItemsInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.TransactWriteItemsOutput, error) {
	m.calls = append(m.calls, params)
	if m.err != nil {
		return nil, m.err
	}
	return &awsdynamodb.TransactWriteItemsOutput{}, nil
}

func (m *mockTxClient) BatchGetItem(ctx context.Context, params *awsdynamodb.BatchGetItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.BatchGetItemOutput, error) {
	return &awsdynamodb.BatchGetItemOutput{}, nil
}

func TestCreateUser_TransactWritePayload(t *testing.T) {
	tx := &mockTxClient{}
	repo := New(nil, tx, "test-table")

	now := time.Now().UTC()
	user := &models.User{
		CustomerID:  "cust_123",
		Email:       "test@example.com",
		FirstName:   "Test",
		LastName:    "User",
		AuthMethods: []string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := repo.CreateUser(context.Background(), user)
	require.NoError(t, err)
	require.Len(t, tx.calls, 1)
	assert.Len(t, tx.calls[0].TransactItems, 2)

	profilePut := tx.calls[0].TransactItems[0].Put
	settingsPut := tx.calls[0].TransactItems[1].Put
	require.NotNil(t, profilePut)
	require.NotNil(t, settingsPut)

	assert.Equal(t, "test-table", *profilePut.TableName)
	assert.Equal(t, "test-table", *settingsPut.TableName)

	skProfile, ok := profilePut.Item["SK"]
	require.True(t, ok)
	assert.Equal(t, "PROFILE", skProfile.(*ddbTypes.AttributeValueMemberS).Value)

	skSettings, ok := settingsPut.Item["SK"]
	require.True(t, ok)
	assert.Equal(t, "SETTINGS", skSettings.(*ddbTypes.AttributeValueMemberS).Value)
}
