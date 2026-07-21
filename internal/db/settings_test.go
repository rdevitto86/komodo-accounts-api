package db

import (
	"context"
	"errors"
	"testing"

	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/test/mocks"
)

func TestUpdateSettingsPartial_MissingRecord_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(ErrNotFound)

	ev := true
	err := repo.UpdateSettingsPartial(context.Background(), "cust_1", &models.UpdateSettingsRequest{EmailVerified: &ev}, 3)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestUpdateSettingsPartial_ExistingRecord_MapsToVersionConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ map[string]ddbTypes.AttributeValue, _ bool, _ []map[string]ddbTypes.AttributeValue, out any) error {
			rec, ok := out.(*settingsRecord)
			require.True(t, ok)
			rec.Version = 2
			return nil
		})

	ev := true
	err := repo.UpdateSettingsPartial(context.Background(), "cust_1", &models.UpdateSettingsRequest{EmailVerified: &ev}, 3)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrVersionConflict))
}

func TestUpdateSettingsPartial_TransientReadError_NotMaskedAsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(errors.New("throttled request"))

	ev := true
	err := repo.UpdateSettingsPartial(context.Background(), "cust_1", &models.UpdateSettingsRequest{EmailVerified: &ev}, 3)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
	assert.False(t, errors.Is(err, ErrVersionConflict))
}

func TestMutateSettingsTags_MissingRecord_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(ErrNotFound)

	err := repo.MutateSettingsTags(context.Background(), "cust_1", &models.UpdateSettingsTagsRequest{Add: []string{"loyalty.vip"}}, 3)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestMutateSettingsTags_ExistingRecord_MapsToVersionConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ map[string]ddbTypes.AttributeValue, _ bool, _ []map[string]ddbTypes.AttributeValue, out any) error {
			rec, ok := out.(*settingsRecord)
			require.True(t, ok)
			rec.Version = 5
			return nil
		})

	err := repo.MutateSettingsTags(context.Background(), "cust_1", &models.UpdateSettingsTagsRequest{Add: []string{"loyalty.vip"}}, 3)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrVersionConflict))
}

func TestMutateSettingsTags_TransientReadError_NotMaskedAsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(errors.New("throttled request"))

	err := repo.MutateSettingsTags(context.Background(), "cust_1", &models.UpdateSettingsTagsRequest{Add: []string{"loyalty.vip"}}, 3)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
	assert.False(t, errors.Is(err, ErrVersionConflict))
}

func TestSoftDeleteAccount_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ map[string]ddbTypes.AttributeValue, _ string, _ map[string]ddbTypes.AttributeValue, _ map[string]string, cond *string) (map[string]ddbTypes.AttributeValue, error) {
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_exists(SK) AND #st <> :st", *cond)
			return nil, nil
		})

	err := repo.SoftDeleteAccount(context.Background(), "cust_1")
	require.NoError(t, err)
}

func TestSoftDeleteAccount_AlreadyPendingDeletion_IsIdempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ map[string]ddbTypes.AttributeValue, _ bool, _ []map[string]ddbTypes.AttributeValue, out any) error {
			rec, ok := out.(*settingsRecord)
			require.True(t, ok)
			rec.Status = "pending_deletion"
			return nil
		})

	err := repo.SoftDeleteAccount(context.Background(), "cust_1")
	require.NoError(t, err)
}

func TestSoftDeleteAccount_MissingRecord_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(ErrNotFound)

	err := repo.SoftDeleteAccount(context.Background(), "cust_1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestSoftDeleteAccount_TransientReadError_NotMaskedAsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")
	key := settingsTestKey()

	client.EXPECT().BuildKey("PK", "ACCOUNT#cust_1", "SK", "SETTINGS").Return(key, nil)
	client.EXPECT().
		UpdateItem(gomock.Any(), "test-table", key, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, conditionalCheckErr())
	client.EXPECT().
		GetItemAs(gomock.Any(), "test-table", key, false, nil, gomock.Any()).
		Return(errors.New("throttled request"))

	err := repo.SoftDeleteAccount(context.Background(), "cust_1")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

// ── Setup ──────────────────────────────────────────────────────────────────

func settingsTestKey() map[string]ddbTypes.AttributeValue {
	return map[string]ddbTypes.AttributeValue{
		"PK": &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#cust_1"},
		"SK": &ddbTypes.AttributeValueMemberS{Value: "SETTINGS"},
	}
}
