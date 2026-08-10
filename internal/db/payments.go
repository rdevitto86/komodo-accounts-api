package db

import (
	"context"
	"errors"
	"fmt"
	"komodo-accounts-api/internal/models"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	"github.com/segmentio/ksuid"
)

type paymentRecord struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	PaymentID   string `dynamodbav:"payment_id"`
	Provider    string `dynamodbav:"provider"`
	Last4       string `dynamodbav:"last4"`
	Brand       string `dynamodbav:"brand"`
	ExpiryMonth int    `dynamodbav:"expiry_month"`
	ExpiryYear  int    `dynamodbav:"expiry_year"`
	IsDefault   bool   `dynamodbav:"is_default"`
}

// DB client method that retrieves a payment method for an account.
func (r *Repo) GetPayment(ctx context.Context, accountID, paymentID string) (*models.PaymentMethod, error) {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", paySK(paymentID))
	if err != nil {
		return nil, fmt.Errorf("failed to build payment key: %w", err)
	}

	var record paymentRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get payment: %w", err)
	}

	pm := record.toModel()
	return &pm, nil
}

// DB client method that upserts a payment method for an account.
func (r *Repo) UpsertPayment(ctx context.Context, accountID string, method *models.PaymentMethod) error {
	isCreate := method.PaymentId == ""
	if isCreate {
		method.PaymentId = "pay_" + ksuid.New().String()
	}

	record := paymentRecordFrom(createPK(accountID), paySK(method.PaymentId), method)

	condition := "attribute_exists(SK)"
	if isCreate {
		condition = "attribute_not_exists(SK)"
	}

	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			if isCreate {
				return fmt.Errorf("failed to create payment method: %w", ErrAlreadyExists)
			}
			return fmt.Errorf("failed to update payment method: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to upsert payment: %w", err)
	}
	return nil
}

// DB method that lists payment methods for an account.
func (r *Repo) ListPayments(ctx context.Context, accountID string) ([]models.PaymentMethod, error) {
	var records []paymentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PAY#"},
		},
		Limit: aws.Int32(100),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to list payments: %w", err)
	}

	methods := make([]models.PaymentMethod, len(records))
	for i := range records {
		methods[i] = records[i].toModel()
	}
	return methods, nil
}

// DB method that deletes a payment method for an account.
func (r *Repo) DeletePayment(ctx context.Context, accountID, paymentID string) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", paySK(paymentID))
	if err != nil {
		return fmt.Errorf("failed to build delete payment key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete payment: %w", err)
	}
	return nil
}

// DB method that sets a payment method as default for an account.
func (r *Repo) SetPaymentDefault(ctx context.Context, accountID, paymentID string) error {
	if err := r.demoteAndPromoteDefault(ctx, createPK(accountID), "PAY#", paySK(paymentID)); err != nil {
		return fmt.Errorf("failed to set payment default: %w", err)
	}
	return nil
}

type defaultFlagRecord struct {
	SK        string `dynamodbav:"SK"`
	IsDefault bool   `dynamodbav:"is_default"`
}

// Helper that demotes existing default addresses and promotes a new one.
func (r *Repo) demoteAndPromoteDefault(ctx context.Context, pk, skPrefix, targetSK string) error {
	var candidates []defaultFlagRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: pk},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: skPrefix},
		},
	}

	if _, err := r.client.QueryAs(ctx, query, &candidates); err != nil {
		return fmt.Errorf("failed to query default candidates: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	condition := aws.String("attribute_exists(SK)")
	transactItems := make([]dynamodb.TransactItem, 0, len(candidates)+1)

	for _, c := range candidates {
		if c.SK == targetSK || !c.IsDefault {
			continue
		}

		transactItems = append(transactItems, dynamodb.TransactItem{
			Table: r.table,
			Op:    dynamodb.TransactUpdate,
			Key: map[string]ddbTypes.AttributeValue{
				"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
				"SK": &ddbTypes.AttributeValueMemberS{Value: c.SK},
			},
			UpdateExpr: "SET #isd = :false, #ua = :now",
			ExprNames: map[string]string{
				"#isd": "is_default",
				"#ua":  "updated_at",
			},
			ExprValues: map[string]ddbTypes.AttributeValue{
				":false": &ddbTypes.AttributeValueMemberBOOL{Value: false},
				":now":   &ddbTypes.AttributeValueMemberS{Value: now},
			},
			Condition: condition,
		})
	}

	transactItems = append(transactItems, dynamodb.TransactItem{
		Table: r.table,
		Op:    dynamodb.TransactUpdate,
		Key: map[string]ddbTypes.AttributeValue{
			"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
			"SK": &ddbTypes.AttributeValueMemberS{Value: targetSK},
		},
		UpdateExpr: "SET #isd = :true, #ua = :now",
		ExprNames: map[string]string{
			"#isd": "is_default",
			"#ua":  "updated_at",
		},
		ExprValues: map[string]ddbTypes.AttributeValue{
			":true": &ddbTypes.AttributeValueMemberBOOL{Value: true},
			":now":  &ddbTypes.AttributeValueMemberS{Value: now},
		},
		Condition: condition,
	})

	if err := r.client.TransactWrite(ctx, transactItems); err != nil {
		if _, ok := dynamodb.ConditionFailureIndex(err); ok {
			return fmt.Errorf("default target item not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to write default state change: %w", err)
	}
	return nil
}

// Helper that converts a payment record to a PaymentMethod.
func (r *paymentRecord) toModel() models.PaymentMethod {
	return models.PaymentMethod{
		PaymentId:   r.PaymentID,
		Provider:    r.Provider,
		Last4:       r.Last4,
		Brand:       r.Brand,
		ExpiryMonth: r.ExpiryMonth,
		ExpiryYear:  r.ExpiryYear,
		IsDefault:   r.IsDefault,
	}
}

// Helper that creates a payment record from a PaymentMethod.
func paymentRecordFrom(pk, sk string, m *models.PaymentMethod) paymentRecord {
	return paymentRecord{
		PK:          pk,
		SK:          sk,
		PaymentID:   m.PaymentId,
		Provider:    m.Provider,
		Last4:       m.Last4,
		Brand:       m.Brand,
		ExpiryMonth: m.ExpiryMonth,
		ExpiryYear:  m.ExpiryYear,
		IsDefault:   m.IsDefault,
	}
}

func paySK(paymentID string) string { return "PAY#" + paymentID }
