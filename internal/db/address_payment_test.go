package db

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/test/mocks"
)

func TestCreateAddress_ConditionalPut_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		WriteItemFrom(gomock.Any(), "test-table", gomock.Any(), false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, _ bool, _ any, cond *string) error {
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_not_exists(SK)", *cond)
			return conditionalCheckErr()
		})

	err := repo.CreateAddress(context.Background(), "cust_1", &models.Address{AddressID: "addr_existing"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestCreateAddress_Success_ServerGeneratedID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		WriteItemFrom(gomock.Any(), "test-table", gomock.Any(), false, nil, gomock.Any()).
		Return(nil)

	addr := &models.Address{}
	err := repo.CreateAddress(context.Background(), "cust_1", addr)
	require.NoError(t, err)
	assert.NotEmpty(t, addr.AddressID)
}

func TestUpsertPayment_CreateIntent_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		WriteItemFrom(gomock.Any(), "test-table", gomock.Any(), false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, _ bool, _ any, cond *string) error {
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_not_exists(SK)", *cond)
			return conditionalCheckErr()
		})

	err := repo.UpsertPayment(context.Background(), "cust_1", &models.PaymentMethod{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

func TestUpsertPayment_UpdateIntent_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		WriteItemFrom(gomock.Any(), "test-table", gomock.Any(), false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, _ bool, _ any, cond *string) error {
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_exists(SK)", *cond)
			return conditionalCheckErr()
		})

	err := repo.UpsertPayment(context.Background(), "cust_1", &models.PaymentMethod{PaymentID: "pay_foreign"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestUpsertPayment_UpdateIntent_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		WriteItemFrom(gomock.Any(), "test-table", gomock.Any(), false, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, _ bool, _ any, cond *string) error {
			require.NotNil(t, cond)
			assert.Equal(t, "attribute_exists(SK)", *cond)
			return nil
		})

	err := repo.UpsertPayment(context.Background(), "cust_1", &models.PaymentMethod{PaymentID: "pay_existing"})
	require.NoError(t, err)
}
