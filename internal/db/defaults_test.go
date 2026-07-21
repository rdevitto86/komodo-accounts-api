package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"

	"komodo-accounts-api/test/mocks"
)

func TestDemoteAndPromoteDefault_DemotesCorrectItemRegardlessOfScanOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{
				{SK: "ADDR#not-default", IsDefault: false},
				{SK: "ADDR#target", IsDefault: false},
				{SK: "ADDR#actual-default", IsDefault: true},
			}
			return &dynamodb.QueryOutput{}, nil
		})

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	err := repo.SetAddressDefault(context.Background(), "cust_1", "target")
	require.NoError(t, err)
	require.Len(t, captured, 2)

	var demotedSK, promotedSK string
	for _, item := range captured {
		sk := item.Key["SK"].(*ddbTypes.AttributeValueMemberS).Value
		if _, promoting := item.ExprValues[":true"]; promoting {
			promotedSK = sk
		} else {
			demotedSK = sk
		}
	}
	assert.Equal(t, "ADDR#actual-default", demotedSK)
	assert.Equal(t, "ADDR#target", promotedSK)
}

func TestDemoteAndPromoteDefault_NoExistingDefault_OnlyPromotes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{
				{SK: "PAY#target", IsDefault: false},
			}
			return &dynamodb.QueryOutput{}, nil
		})

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	err := repo.SetPaymentDefault(context.Background(), "cust_1", "target")
	require.NoError(t, err)
	require.Len(t, captured, 1)
	assert.Equal(t, "PAY#target", captured[0].Key["SK"].(*ddbTypes.AttributeValueMemberS).Value)
}

func TestDemoteAndPromoteDefault_DemotesAllLegacyDuplicates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{
				{SK: "ADDR#legacy-1", IsDefault: true},
				{SK: "ADDR#legacy-2", IsDefault: true},
				{SK: "ADDR#target", IsDefault: false},
			}
			return &dynamodb.QueryOutput{}, nil
		})

	var captured []dynamodb.TransactItem
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []dynamodb.TransactItem) error {
			captured = items
			return nil
		})

	err := repo.SetAddressDefault(context.Background(), "cust_1", "target")
	require.NoError(t, err)
	assert.Len(t, captured, 3)
}

func TestSetAddressDefault_TargetMissing_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{
				{SK: "ADDR#existing-default", IsDefault: true},
			}
			return &dynamodb.QueryOutput{}, nil
		})
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("ConditionalCheckFailed")},
			},
		})

	err := repo.SetAddressDefault(context.Background(), "cust_1", "deleted-between-check-and-write")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestSetPaymentDefault_TargetMissing_MapsToNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{}
			return &dynamodb.QueryOutput{}, nil
		})
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("None")},
				{Code: aws.String("ConditionalCheckFailed")},
			},
		})

	err := repo.SetPaymentDefault(context.Background(), "cust_1", "deleted-between-check-and-write")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestDemoteAndPromoteDefault_NonConditionalCancellation_PropagatesAsRetryable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			records, ok := out.(*[]defaultFlagRecord)
			require.True(t, ok)
			*records = []defaultFlagRecord{}
			return &dynamodb.QueryOutput{}, nil
		})
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(&ddbTypes.TransactionCanceledException{
			CancellationReasons: []ddbTypes.CancellationReason{
				{Code: aws.String("ThrottlingError")},
			},
		})

	err := repo.SetAddressDefault(context.Background(), "cust_1", "target")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

func TestSetAddressDefault_WrapsQueryError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("query failed"))

	err := repo.SetAddressDefault(context.Background(), "cust_1", "target")
	require.Error(t, err)
}

func TestSetPaymentDefault_WrapsTransactError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mocks.NewMockAPI(ctrl)
	repo := New(client, "test-table")

	client.EXPECT().
		QueryAs(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ dynamodb.QueryInput, out any) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{}, nil
		})
	client.EXPECT().
		TransactWrite(gomock.Any(), gomock.Any()).
		Return(errors.New("transact failed"))

	err := repo.SetPaymentDefault(context.Background(), "cust_1", "target")
	require.Error(t, err)
}
