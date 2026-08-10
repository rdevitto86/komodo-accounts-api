package db

import (
	"context"
	"errors"
	"fmt"
	"komodo-accounts-api/internal/models"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	"github.com/segmentio/ksuid"
)

type addressRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	AddressID string `dynamodbav:"address_id"`
	Alias     string `dynamodbav:"alias,omitempty"`
	Line1     string `dynamodbav:"line1"`
	Line2     string `dynamodbav:"line2,omitempty"`
	City      string `dynamodbav:"city"`
	State     string `dynamodbav:"state"`
	ZipCode   string `dynamodbav:"zip_code"`
	Country   string `dynamodbav:"country"`
	IsDefault bool   `dynamodbav:"is_default"`
}

// DB method that creates a new address for an account.
func (r *Repo) CreateAddress(ctx context.Context, accountID string, addr *models.Address) error {
	if addr.AddressId == "" {
		addr.AddressId = "addr_" + ksuid.New().String()
	}

	record := addressRecordFrom(createPK(accountID), addrSK(addr.AddressId), addr)

	condition := "attribute_not_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("failed to create address: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create address: %w", err)
	}
	return nil
}

// DB method that retrieves an address for an account.
func (r *Repo) GetAddress(ctx context.Context, accountID, addressID string) (*models.Address, error) {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", addrSK(addressID))
	if err != nil {
		return nil, fmt.Errorf("failed to build address key: %w", err)
	}

	var record addressRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get address: %w", err)
	}
	addr := record.toModel()
	return &addr, nil
}

// DB method that retrieves all addresses for an account.
func (r *Repo) GetAccountAddresses(ctx context.Context, accountID string) ([]models.Address, error) {
	var records []addressRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "ADDR#"},
		},
		Limit: aws.Int32(100),
	}
	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to get account addresses: %w", err)
	}

	addrs := make([]models.Address, len(records))
	for i := range records {
		addrs[i] = records[i].toModel()
	}
	return addrs, nil
}

// DB method that updates an address for an account.
func (r *Repo) UpdateAddress(ctx context.Context, accountID, addressID string, req *models.UpdateAddressRequest) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("failed to build address key: %w", err)
	}

	var setClauses []string
	exprValues := map[string]ddbTypes.AttributeValue{}
	exprNames := map[string]string{}

	if req.Alias != nil {
		setClauses = append(setClauses, "#alias = :alias")
		exprValues[":alias"] = &ddbTypes.AttributeValueMemberS{Value: *req.Alias}
		exprNames["#alias"] = "alias"
	}
	if req.Line1 != nil {
		setClauses = append(setClauses, "#l1 = :l1")
		exprValues[":l1"] = &ddbTypes.AttributeValueMemberS{Value: *req.Line1}
		exprNames["#l1"] = "line1"
	}
	if req.Line2 != nil {
		setClauses = append(setClauses, "#l2 = :l2")
		exprValues[":l2"] = &ddbTypes.AttributeValueMemberS{Value: *req.Line2}
		exprNames["#l2"] = "line2"
	}
	if req.City != nil {
		setClauses = append(setClauses, "#city = :city")
		exprValues[":city"] = &ddbTypes.AttributeValueMemberS{Value: *req.City}
		exprNames["#city"] = "city"
	}
	if req.State != nil {
		setClauses = append(setClauses, "#state = :state")
		exprValues[":state"] = &ddbTypes.AttributeValueMemberS{Value: *req.State}
		exprNames["#state"] = "state"
	}
	if req.ZipCode != nil {
		setClauses = append(setClauses, "#zc = :zc")
		exprValues[":zc"] = &ddbTypes.AttributeValueMemberS{Value: *req.ZipCode}
		exprNames["#zc"] = "zip_code"
	}
	if req.Country != nil {
		setClauses = append(setClauses, "#country = :country")
		exprValues[":country"] = &ddbTypes.AttributeValueMemberS{Value: *req.Country}
		exprNames["#country"] = "country"
	}
	if req.IsDefault != nil {
		setClauses = append(setClauses, "#isd = :isd")
		exprValues[":isd"] = &ddbTypes.AttributeValueMemberBOOL{Value: *req.IsDefault}
		exprNames["#isd"] = "is_default"
	}

	if len(setClauses) == 0 {
		return nil
	}

	updateExpr := "SET " + strings.Join(setClauses, ", ")
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("failed to update address: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update address: %w", err)
	}
	return nil
}

// DB method that sets an address as the default for an account.
func (r *Repo) SetAddressDefault(ctx context.Context, accountID, addressID string) error {
	if err := r.demoteAndPromoteDefault(ctx, createPK(accountID), "ADDR#", addrSK(addressID)); err != nil {
		return fmt.Errorf("failed to set address default: %w", err)
	}
	return nil
}

// DB method that deletes an address for an account.
func (r *Repo) DeleteAddress(ctx context.Context, accountID, addressID string) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("failed to build delete address key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}

// Helper that converts an address record to a Address model.
func (r *addressRecord) toModel() models.Address {
	return models.Address{
		AddressId: r.AddressID,
		Alias:     optStr(r.Alias),
		Line1:     r.Line1,
		Line2:     optStr(r.Line2),
		City:      r.City,
		State:     r.State,
		ZipCode:   r.ZipCode,
		Country:   r.Country,
		IsDefault: r.IsDefault,
	}
}

// Helper that creates an address record from a Address model.
func addressRecordFrom(pk, sk string, a *models.Address) addressRecord {
	return addressRecord{
		PK:        pk,
		SK:        sk,
		AddressID: a.AddressId,
		Alias:     deref(a.Alias),
		Line1:     a.Line1,
		Line2:     deref(a.Line2),
		City:      a.City,
		State:     a.State,
		ZipCode:   a.ZipCode,
		Country:   a.Country,
		IsDefault: a.IsDefault,
	}
}

func addrSK(addressID string) string { return "ADDR#" + addressID }
