//go:generate go run go.uber.org/mock/mockgen -destination=../../test/mocks/mock_dynamodb.go -package=mocks github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb API
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/segmentio/ksuid"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

var (
	ErrNotFound                   = dynamodb.ErrNotFound
	ErrAlreadyExists              = errors.New("already exists")
	ErrPasskeyAlreadyExists       = errors.New("passkey already exists")
	ErrPasskeySignCountRegression = errors.New("passkey sign count regression")
	ErrAccountNotPendingDeletion  = errors.New("account not pending deletion")
	ErrVersionConflict            = errors.New("version conflict")
)

type Repo struct {
	client dynamodb.API
	table  string
}

func New(client dynamodb.API, table string) *Repo {
	return &Repo{client: client, table: table}
}

type accountRecord struct {
	PK           string    `dynamodbav:"PK"`
	SK           string    `dynamodbav:"SK"`
	AccountID    string    `dynamodbav:"account_id"`
	Username     string    `dynamodbav:"username"`
	Email        string    `dynamodbav:"email"`
	Phone        string    `dynamodbav:"phone"`
	FirstName    string    `dynamodbav:"first_name"`
	LastName     string    `dynamodbav:"last_name"`
	AvatarURL    string    `dynamodbav:"avatar_url"`
	PasswordHash string    `dynamodbav:"password_hash"`
	AuthMethods  []string  `dynamodbav:"auth_methods"`
	GSI1PK       string    `dynamodbav:"GSI1PK"`
	GSI1SK       string    `dynamodbav:"GSI1SK"`
	CreatedAt    time.Time `dynamodbav:"created_at"`
	UpdatedAt    time.Time `dynamodbav:"updated_at"`
}

func (r *accountRecord) toModel() *models.Account {
	return &models.Account{
		AccountID:   r.AccountID,
		Username:    r.Username,
		Email:       r.Email,
		Phone:       r.Phone,
		FirstName:   r.FirstName,
		LastName:    r.LastName,
		AvatarURL:   r.AvatarURL,
		AuthMethods: r.AuthMethods,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// DB client method that retrieves an account by its ID.
func (r *Repo) GetAccount(ctx context.Context, accountID string) (*models.Account, error) {
	key, err := r.client.BuildKey("PK", "ACCOUNT#"+accountID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build account key: %w", err)
	}

	var record accountRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return record.toModel(), nil
}

type gsiEmailResult struct {
	AccountID string `dynamodbav:"account_id"`
	PK        string `dynamodbav:"PK"`
}

// DB client private method that queries the GSI1 index to resolve an account ID by email.
func (r *Repo) resolveAccountIDByEmail(ctx context.Context, email string) (string, error) {
	gsiName := "GSI1"
	var results []gsiEmailResult
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		IndexName:              &gsiName,
		KeyConditionExpression: "GSI1PK = :pk AND GSI1SK = :sk",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk": &ddbTypes.AttributeValueMemberS{Value: "EMAIL#" + strings.ToLower(email)},
			":sk": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
		},
	}

	if err := r.client.QueryAllAs(ctx, query, &results); err != nil {
		return "", fmt.Errorf("failed to query account by email: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("account not found: %w", ErrNotFound)
	}
	return results[0].AccountID, nil
}

// DB client private method that retrieves an account by its email.
func (r *Repo) getAccountByEmail(ctx context.Context, email string) (*accountRecord, error) {
	accountID, err := r.resolveAccountIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	key, err := r.client.BuildKey("PK", "ACCOUNT#"+accountID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build email lookup key: %w", err)
	}

	var record accountRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get account by email: %w", err)
	}
	return &record, nil
}

type credentialsBatchItem struct {
	SK            string   `dynamodbav:"SK"`
	AccountID     string   `dynamodbav:"account_id"`
	PasswordHash  string   `dynamodbav:"password_hash"`
	AuthMethods   []string `dynamodbav:"auth_methods"`
	EmailVerified bool     `dynamodbav:"email_verified"`
}

// DB client method that retrieves account credentials by email.
func (r *Repo) GetAccountCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	accountID, err := r.resolveAccountIDByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to get account credentials: %w", err)
	}

	pk := "ACCOUNT#" + accountID
	keys := []map[string]ddbTypes.AttributeValue{
		{
			"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
			"SK": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
		},
		{
			"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
			"SK": &ddbTypes.AttributeValueMemberS{Value: "SETTINGS"},
		},
	}

	var items []credentialsBatchItem
	if err := r.client.BatchGetItemAs(ctx, r.table, keys, &items); err != nil {
		return nil, fmt.Errorf("failed to batch get credentials: %w", err)
	}

	var profile credentialsBatchItem
	var settings credentialsBatchItem
	profileFound := false

	for _, item := range items {
		switch item.SK {
		case "PROFILE":
			profile = item
			profileFound = true
		case "SETTINGS":
			settings = item
		}
	}

	if !profileFound {
		return nil, fmt.Errorf("account not found: %w", ErrNotFound)
	}

	return &models.CredentialsResponse{
		AccountID:     profile.AccountID,
		PasswordHash:  profile.PasswordHash,
		EmailVerified: settings.EmailVerified,
		AuthMethods:   profile.AuthMethods,
	}, nil
}

// DB client method that updates account credentials.
func (r *Repo) UpdateAccountCredentials(ctx context.Context, accountID string, req *models.UpdateCredentialsRequest) error {
	key, err := r.client.BuildKey("PK", "ACCOUNT#"+accountID, "SK", "PROFILE")
	if err != nil {
		return fmt.Errorf("failed to build credentials key: %w", err)
	}

	setClauses := []string{}
	exprValues := map[string]ddbTypes.AttributeValue{}
	exprNames := map[string]string{}

	if req.PasswordHash != "" {
		setClauses = append(setClauses, "#ph = :ph")
		exprValues[":ph"] = &ddbTypes.AttributeValueMemberS{Value: req.PasswordHash}
		exprNames["#ph"] = "password_hash"
	}

	if len(req.AuthMethods) > 0 {
		amList := make([]ddbTypes.AttributeValue, len(req.AuthMethods))
		for i, m := range req.AuthMethods {
			amList[i] = &ddbTypes.AttributeValueMemberS{Value: m}
		}
		setClauses = append(setClauses, "#am = :am")
		exprValues[":am"] = &ddbTypes.AttributeValueMemberL{Value: amList}
		exprNames["#am"] = "auth_methods"
	}

	setClauses = append(setClauses, "#ua = :ua")
	exprValues[":ua"] = &ddbTypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)}
	exprNames["#ua"] = "updated_at"

	updateExpr := "SET " + strings.Join(setClauses, ", ")

	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key,
		updateExpr,
		exprValues,
		exprNames,
		&condition,
	); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("failed to update credentials: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update credentials: %w", err)
	}
	return nil
}

// DB client method that checks if an account exists by email.
func (r *Repo) GetAccountExistsByEmail(ctx context.Context, email string) (*models.AccountExistsResponse, error) {
	record, err := r.getAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &models.AccountExistsResponse{Exists: false, AuthMethods: []string{}}, nil
		}
		return nil, fmt.Errorf("failed to check account exists: %w", err)
	}
	return &models.AccountExistsResponse{
		Exists:      true,
		AuthMethods: record.AuthMethods,
	}, nil
}

type settingsDefaultRecord struct {
	PK            string    `dynamodbav:"PK"`
	SK            string    `dynamodbav:"SK"`
	Status        string    `dynamodbav:"status"`
	EmailVerified bool      `dynamodbav:"email_verified"`
	PhoneVerified bool      `dynamodbav:"phone_verified"`
	Version       int       `dynamodbav:"version"`
	CreatedAt     time.Time `dynamodbav:"created_at"`
	UpdatedAt     time.Time `dynamodbav:"updated_at"`
}

// DB client method that creates a new account.
func (r *Repo) CreateAccount(ctx context.Context, account *models.Account) error {
	profileRec := accountRecord{
		PK:           "ACCOUNT#" + account.AccountID,
		SK:           "PROFILE",
		GSI1PK:       "EMAIL#" + strings.ToLower(account.Email),
		GSI1SK:       "PROFILE",
		AccountID:    account.AccountID,
		Username:     account.Username,
		Email:        strings.ToLower(account.Email),
		Phone:        account.Phone,
		FirstName:    account.FirstName,
		LastName:     account.LastName,
		AvatarURL:    account.AvatarURL,
		PasswordHash: account.PasswordHash,
		AuthMethods:  account.AuthMethods,
		CreatedAt:    account.CreatedAt,
		UpdatedAt:    account.UpdatedAt,
	}
	settingsRec := settingsDefaultRecord{
		PK:        "ACCOUNT#" + account.AccountID,
		SK:        "SETTINGS",
		Status:    "active",
		Version:   0,
		CreatedAt: account.CreatedAt,
		UpdatedAt: account.UpdatedAt,
	}

	condition := aws.String("attribute_not_exists(SK)")
	err := r.client.TransactWrite(ctx, []dynamodb.TransactItem{
		{Table: r.table, Op: dynamodb.TransactPut, Item: profileRec, Condition: condition},
		{Table: r.table, Op: dynamodb.TransactPut, Item: settingsRec, Condition: condition},
	})
	if err != nil {
		if _, ok := dynamodb.ConditionFailureIndex(err); ok {
			return fmt.Errorf("failed to create account: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create account: %w", err)
	}
	return nil
}

// DB client method that updates an existing account.
func (r *Repo) UpdateAccount(ctx context.Context, accountID string, update *models.UpdateProfileRequest) (*models.Account, error) {
	key, err := r.client.BuildKey("PK", "ACCOUNT#"+accountID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build update key: %w", err)
	}

	setClauses := []string{}
	exprValues := map[string]ddbTypes.AttributeValue{}
	exprNames := map[string]string{}

	if update.Phone != nil {
		setClauses = append(setClauses, "#ph = :ph")
		exprValues[":ph"] = &ddbTypes.AttributeValueMemberS{Value: *update.Phone}
		exprNames["#ph"] = "phone"
	}
	if update.FirstName != nil {
		setClauses = append(setClauses, "#fn = :fn")
		exprValues[":fn"] = &ddbTypes.AttributeValueMemberS{Value: *update.FirstName}
		exprNames["#fn"] = "first_name"
	}
	if update.LastName != nil {
		setClauses = append(setClauses, "#ln = :ln")
		exprValues[":ln"] = &ddbTypes.AttributeValueMemberS{Value: *update.LastName}
		exprNames["#ln"] = "last_name"
	}
	if update.AvatarURL != nil {
		setClauses = append(setClauses, "#av = :av")
		exprValues[":av"] = &ddbTypes.AttributeValueMemberS{Value: *update.AvatarURL}
		exprNames["#av"] = "avatar_url"
	}

	setClauses = append(setClauses, "#ua = :ua")
	exprValues[":ua"] = &ddbTypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)}
	exprNames["#ua"] = "updated_at"

	updateExpr := "SET " + strings.Join(setClauses, ", ")
	condition := "attribute_exists(SK)"

	var updated accountRecord
	if err := r.client.UpdateItemAs(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition, &updated); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update account: %w", err)
	}
	return updated.toModel(), nil
}

// DB client method that deletes an existing account.
func (r *Repo) DeleteAccount(ctx context.Context, accountID string) error {
	items, err := r.client.QueryAll(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk": &ddbTypes.AttributeValueMemberS{Value: "ACCOUNT#" + accountID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query account items: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	if len(items) > 100 {
		logger.Warn("large account delete; processing in chunks",
			logger.Attr("account_id", accountID),
			logger.Attr("item_count", len(items)),
		)
	}

	const chunkSize = 100
	for start := 0; start < len(items); start += chunkSize {
		end := start + chunkSize
		if end > len(items) {
			end = len(items)
		}

		transactItems := make([]dynamodb.TransactItem, 0, end-start)
		for _, item := range items[start:end] {
			pk, hasPK := item["PK"]
			sk, hasSK := item["SK"]
			if !hasPK || !hasSK {
				continue
			}
			transactItems = append(transactItems, dynamodb.TransactItem{
				Table: r.table,
				Op:    dynamodb.TransactDelete,
				Key: map[string]ddbTypes.AttributeValue{
					"PK": pk,
					"SK": sk,
				},
			})
		}

		if len(transactItems) == 0 {
			continue
		}

		if err := r.client.TransactWrite(ctx, transactItems); err != nil {
			return fmt.Errorf("failed to delete account: %w", err)
		}
	}
	return nil
}

type addressRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Address
}

// Helper function that returns a formatted partition key for addresses
func addrPK(accountID string) string { return "ACCOUNT#" + accountID }

// Helper function that returns a formatted sort key for addresses
func addrSK(addressID string) string { return "ADDR#" + addressID }

// DB client method that creates a new address for an account.
func (r *Repo) CreateAddress(ctx context.Context, accountID string, addr *models.Address) error {
	if addr.AddressID == "" {
		addr.AddressID = "addr_" + ksuid.New().String()
	}

	record := addressRecord{
		PK:      addrPK(accountID),
		SK:      addrSK(addr.AddressID),
		Address: *addr,
	}

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

// DB client method that retrieves an address for an account.
func (r *Repo) GetAddress(ctx context.Context, accountID, addressID string) (*models.Address, error) {
	key, err := r.client.BuildKey("PK", addrPK(accountID), "SK", addrSK(addressID))
	if err != nil {
		return nil, fmt.Errorf("failed to build address key: %w", err)
	}

	var record addressRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get address: %w", err)
	}
	addr := record.Address
	return &addr, nil
}

// DB client method that retrieves all addresses for an account.
func (r *Repo) GetAccountAddresses(ctx context.Context, accountID string) ([]models.Address, error) {
	var records []addressRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: addrPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "ADDR#"},
		},
		Limit: aws.Int32(100),
	}
	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to get account addresses: %w", err)
	}

	addrs := make([]models.Address, len(records))
	for i, r := range records {
		addrs[i] = r.Address
	}
	return addrs, nil
}

// DB client method that updates an address for an account.
func (r *Repo) UpdateAddress(ctx context.Context, accountID, addressID string, req *models.UpdateAddressRequest) error {
	key, err := r.client.BuildKey("PK", addrPK(accountID), "SK", addrSK(addressID))
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

// DB client method that deletes an address for an account.
func (r *Repo) DeleteAddress(ctx context.Context, accountID, addressID string) error {
	key, err := r.client.BuildKey("PK", addrPK(accountID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("failed to build delete address key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}

type defaultFlagRecord struct {
	SK        string `dynamodbav:"SK"`
	IsDefault bool   `dynamodbav:"is_default"`
}

// Helper function that demotes existing default addresses and promotes a new one.
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

func (r *Repo) SetAddressDefault(ctx context.Context, accountID, addressID string) error {
	if err := r.demoteAndPromoteDefault(ctx, addrPK(accountID), "ADDR#", addrSK(addressID)); err != nil {
		return fmt.Errorf("failed to set address default: %w", err)
	}
	return nil
}

type paymentRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.PaymentMethod
}

// Helper function that formats the partition key for a payment record.
func payPK(accountID string) string { return "ACCOUNT#" + accountID }

// Helper function that formats the sort key for a payment record.
func paySK(paymentID string) string { return "PAY#" + paymentID }

// DB client method that upserts a payment method for an account.
func (r *Repo) UpsertPayment(ctx context.Context, accountID string, method *models.PaymentMethod) error {
	isCreate := method.PaymentID == ""
	if isCreate {
		method.PaymentID = "pay_" + ksuid.New().String()
	}

	record := paymentRecord{
		PK:            payPK(accountID),
		SK:            paySK(method.PaymentID),
		PaymentMethod: *method,
	}

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

// DB client method that retrieves a payment method for an account.
func (r *Repo) GetPayment(ctx context.Context, accountID, paymentID string) (*models.PaymentMethod, error) {
	key, err := r.client.BuildKey("PK", payPK(accountID), "SK", paySK(paymentID))
	if err != nil {
		return nil, fmt.Errorf("failed to build payment key: %w", err)
	}

	var record paymentRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get payment: %w", err)
	}

	pm := record.PaymentMethod
	pm.Token = ""
	return &pm, nil
}

func (r *Repo) ListPayments(ctx context.Context, accountID string) ([]models.PaymentMethod, error) {
	var records []paymentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: payPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PAY#"},
		},
		Limit: aws.Int32(100),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to list payments: %w", err)
	}

	methods := make([]models.PaymentMethod, len(records))
	for i, r := range records {
		pm := r.PaymentMethod
		pm.Token = ""
		methods[i] = pm
	}
	return methods, nil
}

// DB client method that deletes a payment method for an account.
func (r *Repo) DeletePayment(ctx context.Context, accountID, paymentID string) error {
	key, err := r.client.BuildKey("PK", payPK(accountID), "SK", paySK(paymentID))
	if err != nil {
		return fmt.Errorf("failed to build delete payment key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete payment: %w", err)
	}
	return nil
}

// DB client method that sets a payment method as default for an account.
func (r *Repo) SetPaymentDefault(ctx context.Context, accountID, paymentID string) error {
	if err := r.demoteAndPromoteDefault(ctx, payPK(accountID), "PAY#", paySK(paymentID)); err != nil {
		return fmt.Errorf("failed to set payment default: %w", err)
	}
	return nil
}

type prefsRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Preferences
}

// Helper function that formats the partition key for a preferences record.
func prefsPK(accountID string) string { return "ACCOUNT#" + accountID }

// DB client method that retrieves account preferences.
func (r *Repo) GetAccountPreferences(ctx context.Context, accountID string) (*models.Preferences, error) {
	key, err := r.client.BuildKey("PK", prefsPK(accountID), "SK", "PREFS")
	if err != nil {
		return nil, fmt.Errorf("failed to build preferences key: %w", err)
	}

	var record prefsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get preferences: %w", err)
	}
	prefs := record.Preferences
	return &prefs, nil
}

// DB client method that updates account preferences.
func (r *Repo) UpdateAccountPreferences(ctx context.Context, accountID string, req *models.UpdatePreferencesRequest) error {
	key, err := r.client.BuildKey("PK", prefsPK(accountID), "SK", "PREFS")
	if err != nil {
		return fmt.Errorf("failed to build preferences key: %w", err)
	}

	var setClauses []string
	exprValues := map[string]ddbTypes.AttributeValue{}
	exprNames := map[string]string{}

	if req.Language != nil {
		setClauses = append(setClauses, "#lang = :lang")
		exprValues[":lang"] = &ddbTypes.AttributeValueMemberS{Value: *req.Language}
		exprNames["#lang"] = "language"
	}
	if req.Timezone != nil {
		setClauses = append(setClauses, "#tz = :tz")
		exprValues[":tz"] = &ddbTypes.AttributeValueMemberS{Value: *req.Timezone}
		exprNames["#tz"] = "timezone"
	}
	if req.Communication != nil {
		m := make(map[string]ddbTypes.AttributeValue, len(req.Communication))
		for k, v := range req.Communication {
			m[k] = &ddbTypes.AttributeValueMemberBOOL{Value: v}
		}
		setClauses = append(setClauses, "#comm = :comm")
		exprValues[":comm"] = &ddbTypes.AttributeValueMemberM{Value: m}
		exprNames["#comm"] = "communication"
	}

	if len(setClauses) == 0 {
		return nil
	}

	updateExpr := "SET " + strings.Join(setClauses, ", ")
	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, nil); err != nil {
		return fmt.Errorf("failed to update preferences: %w", err)
	}
	return nil
}

// DB client method that deletes account preferences.
func (r *Repo) DeleteAccountPreferences(ctx context.Context, accountID string) error {
	key, err := r.client.BuildKey("PK", prefsPK(accountID), "SK", "PREFS")
	if err != nil {
		return fmt.Errorf("failed to build delete preferences key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete preferences: %w", err)
	}
	return nil
}

type passkeyRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.PasskeyCredential
}

// Helper function that formats the partition key for a passkey record.
func passkeyPK(accountID string) string { return "ACCOUNT#" + accountID }

// Helper function that formats the sort key for a passkey record.
func passkeySK(credentialID string) string { return "PASSKEY#" + credentialID }

// DB client method that creates a passkey credential for an account.
func (r *Repo) CreatePasskey(ctx context.Context, accountID string, cred *models.PasskeyCredential) error {
	if cred.CredentialID == "" {
		return fmt.Errorf("credential_id is required")
	}

	now := time.Now().UTC()
	cred.CreatedAt = now
	cred.LastUsedAt = nil

	record := passkeyRecord{
		PK:                passkeyPK(accountID),
		SK:                passkeySK(cred.CredentialID),
		PasskeyCredential: *cred,
	}

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

// DB client method that retrieves all passkey credentials for an account.
func (r *Repo) GetAccountPasskeys(ctx context.Context, accountID string) ([]models.PasskeyCredential, error) {
	var records []passkeyRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: passkeyPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PASSKEY#"},
		},
		Limit: aws.Int32(100),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to query passkeys: %w", err)
	}

	creds := make([]models.PasskeyCredential, len(records))
	for i, r := range records {
		creds[i] = r.PasskeyCredential
	}
	return creds, nil
}

// DB client method that updates a passkey credential for an account.
func (r *Repo) UpdatePasskey(ctx context.Context, accountID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error) {
	key, err := r.client.BuildKey("PK", passkeyPK(accountID), "SK", passkeySK(credentialID))
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
	cred := updated.PasskeyCredential
	return &cred, nil
}

// DB client method that deletes a passkey credential for an account.
func (r *Repo) DeletePasskey(ctx context.Context, accountID, credentialID string) error {
	key, err := r.client.BuildKey("PK", passkeyPK(accountID), "SK", passkeySK(credentialID))
	if err != nil {
		return fmt.Errorf("failed to build passkey key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}

type settingsRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.AccountSettings
}

// Helper function that formats the partition key for a settings record.
func settingsPK(accountID string) string { return "ACCOUNT#" + accountID }

// DB client method that retrieves the account settings.
func (r *Repo) GetSettings(ctx context.Context, accountID string) (*models.AccountSettings, error) {
	key, err := r.client.BuildKey("PK", settingsPK(accountID), "SK", "SETTINGS")
	if err != nil {
		return nil, fmt.Errorf("failed to build settings key: %w", err)
	}

	var record settingsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	s := record.AccountSettings
	return &s, nil
}

// DB client method that updates the account settings.
func (r *Repo) UpdateSettingsPartial(ctx context.Context, accountID string, req *models.UpdateSettingsRequest, version int) error {
	key, err := r.client.BuildKey("PK", settingsPK(accountID), "SK", "SETTINGS")
	if err != nil {
		return fmt.Errorf("failed to build settings key: %w", err)
	}

	setClauses := []string{}
	exprValues := map[string]ddbTypes.AttributeValue{
		":expected": &ddbTypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", version)},
		":newV":     &ddbTypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", version+1)},
	}
	exprNames := map[string]string{"#v": "version"}

	if req.EmailVerified != nil {
		setClauses = append(setClauses, "#ev = :ev")
		exprValues[":ev"] = &ddbTypes.AttributeValueMemberBOOL{Value: *req.EmailVerified}
		exprNames["#ev"] = "email_verified"
	}
	if req.EmailVerifiedAt != nil {
		setClauses = append(setClauses, "#evat = :evat")
		exprValues[":evat"] = &ddbTypes.AttributeValueMemberS{Value: req.EmailVerifiedAt.UTC().Format(time.RFC3339Nano)}
		exprNames["#evat"] = "email_verified_at"
	}
	if req.PhoneVerified != nil {
		setClauses = append(setClauses, "#pv = :pv")
		exprValues[":pv"] = &ddbTypes.AttributeValueMemberBOOL{Value: *req.PhoneVerified}
		exprNames["#pv"] = "phone_verified"
	}
	if req.PhoneVerifiedAt != nil {
		setClauses = append(setClauses, "#pvat = :pvat")
		exprValues[":pvat"] = &ddbTypes.AttributeValueMemberS{Value: req.PhoneVerifiedAt.UTC().Format(time.RFC3339Nano)}
		exprNames["#pvat"] = "phone_verified_at"
	}
	if req.Status != nil {
		setClauses = append(setClauses, "#st = :st")
		exprValues[":st"] = &ddbTypes.AttributeValueMemberS{Value: *req.Status}
		exprNames["#st"] = "status"
		setClauses = append(setClauses, "#scat = :scat")
		exprValues[":scat"] = &ddbTypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)}
		exprNames["#scat"] = "status_changed_at"
	}
	if req.StatusReason != nil {
		setClauses = append(setClauses, "#sr = :sr")
		exprValues[":sr"] = &ddbTypes.AttributeValueMemberS{Value: *req.StatusReason}
		exprNames["#sr"] = "status_reason"
	}

	setClauses = append(setClauses, "#ua = :ua", "#v = :newV")
	exprValues[":ua"] = &ddbTypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)}
	exprNames["#ua"] = "updated_at"

	updateExpr := "SET " + strings.Join(setClauses, ", ")
	condition := "attribute_exists(SK) AND #v = :expected"
	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return r.settingsConditionFailureError(ctx, key)
		}
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

// Helper function that returns a descriptive error when the settings condition fails.
func (r *Repo) settingsConditionFailureError(ctx context.Context, key map[string]ddbTypes.AttributeValue) error {
	var existing settingsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("settings not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to verify settings existence: %w", err)
	}
	return fmt.Errorf("failed to write settings due to version conflict: %w", ErrVersionConflict)
}

// DB client method that updates the account settings.
func (r *Repo) UpdateSettings(ctx context.Context, accountID string, s *models.AccountSettings) error {
	record := settingsRecord{
		PK:              settingsPK(accountID),
		SK:              "SETTINGS",
		AccountSettings: *s,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

type consentRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.ConsentLog
}

// Helper function that formats the partition key for a consent record.
func consentPK(accountID string) string { return "ACCOUNT#" + accountID }

const consentSKTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Helper function that formats the sort key for a consent record.
func consentSK(channel string, at time.Time) string {
	return "CONSENT#" + channel + "#" + at.UTC().Format(consentSKTimeFormat)
}

// DB client method that appends a consent log entry for an account.
func (r *Repo) AppendConsentLog(ctx context.Context, accountID string, entry *models.ConsentLog) error {
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	record := consentRecord{
		PK:         consentPK(accountID),
		SK:         consentSK(entry.Channel, entry.RecordedAt),
		ConsentLog: *entry,
	}
	condition := "attribute_not_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		return fmt.Errorf("failed to append consent log: %w", err)
	}
	return nil
}

// DB client method that lists the consent history for an account.
func (r *Repo) ListConsentHistory(ctx context.Context, accountID string) ([]models.ConsentLog, error) {
	var records []consentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :prefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":     &ddbTypes.AttributeValueMemberS{Value: consentPK(accountID)},
			":prefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#"},
		},
		Limit: aws.Int32(1000),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to list consent history: %w", err)
	}

	logs := make([]models.ConsentLog, len(records))
	for i, r := range records {
		logs[i] = r.ConsentLog
	}
	return logs, nil
}

// DB client method that modifies the tags on an account's settings.
func (r *Repo) MutateSettingsTags(ctx context.Context, accountID string, req *models.UpdateSettingsTagsRequest, version int) error {
	if len(req.Add) == 0 && len(req.Remove) == 0 {
		return nil
	}

	key, err := r.client.BuildKey("PK", settingsPK(accountID), "SK", "SETTINGS")
	if err != nil {
		return fmt.Errorf("failed to build settings key for tag mutation: %w", err)
	}

	exprValues := map[string]ddbTypes.AttributeValue{
		":expected": &ddbTypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", version)},
		":newV":     &ddbTypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", version+1)},
	}
	exprNames := map[string]string{
		"#tags": "tags",
		"#v":    "version",
	}

	clauses := []string{"SET #v = :newV"}

	if len(req.Add) > 0 {
		addVals := make([]string, len(req.Add))
		copy(addVals, req.Add)
		clauses = append(clauses, "ADD #tags :addSet")
		exprValues[":addSet"] = &ddbTypes.AttributeValueMemberSS{Value: addVals}
	}
	if len(req.Remove) > 0 {
		removeVals := make([]string, len(req.Remove))
		copy(removeVals, req.Remove)
		clauses = append(clauses, "DELETE #tags :removeSet")
		exprValues[":removeSet"] = &ddbTypes.AttributeValueMemberSS{Value: removeVals}
	}

	updateExpr := strings.Join(clauses, " ")
	condition := "attribute_exists(SK) AND #v = :expected"
	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return r.settingsConditionFailureError(ctx, key)
		}
		return fmt.Errorf("failed to mutate settings tags: %w", err)
	}
	return nil
}

// DB client method that retrieves the latest consent record for a given channel.
func (r *Repo) GetLatestConsent(ctx context.Context, accountID, channel string) (*models.ConsentLog, error) {
	var records []consentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: consentPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#" + channel + "#"},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(1),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to query consent log: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0].ConsentLog, nil
}

// Helper function for backward compatibility with old query method.
func (r *Repo) getLatestConsentLegacy(ctx context.Context, accountID, channel string) (*models.ConsentLog, error) {
	var records []consentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: consentPK(accountID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#" + channel + "#"},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(1),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to query consent log: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("consent log not found: %w", ErrNotFound)
	}

	entry := records[0].ConsentLog
	return &entry, nil
}

// DB client method that soft deletes an account by setting its status to "pending_deletion".
func (r *Repo) SoftDeleteAccount(ctx context.Context, accountID string) error {
	key, err := r.client.BuildKey("PK", settingsPK(accountID), "SK", "SETTINGS")
	if err != nil {
		return fmt.Errorf("failed to build settings key: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	exprValues := map[string]ddbTypes.AttributeValue{
		":st":   &ddbTypes.AttributeValueMemberS{Value: "pending_deletion"},
		":scat": &ddbTypes.AttributeValueMemberS{Value: now},
		":ua":   &ddbTypes.AttributeValueMemberS{Value: now},
	}
	exprNames := map[string]string{
		"#st":   "status",
		"#scat": "status_changed_at",
		"#ua":   "updated_at",
	}
	updateExpr := "SET #st = :st, #scat = :scat, #ua = :ua"
	condition := "attribute_exists(SK) AND #st <> :st"

	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			var existing settingsRecord
			if getErr := r.client.GetItemAs(ctx, r.table, key, false, nil, &existing); getErr != nil {
				if errors.Is(getErr, ErrNotFound) {
					return fmt.Errorf("settings record not found: %w", ErrNotFound)
				}
				return fmt.Errorf("failed to verify settings existence: %w", getErr)
			}
			if existing.Status == "pending_deletion" {
				return nil
			}
			return fmt.Errorf("failed to soft-delete account: %w", err)
		}
		return fmt.Errorf("failed to soft-delete account: %w", err)
	}
	return nil
}

// DB client method that restores a pending deletion account by setting its status back to "active".
func (r *Repo) RestoreAccount(ctx context.Context, accountID string) error {
	settings, err := r.GetSettings(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get settings for restore: %w", err)
	}

	if settings.Status != "pending_deletion" {
		return fmt.Errorf("account not in pending deletion state: %w", ErrAccountNotPendingDeletion)
	}
	if settings.StatusChangedAt == nil || time.Since(*settings.StatusChangedAt) > 30*24*time.Hour {
		return fmt.Errorf("account restore window expired: %w", ErrAccountNotPendingDeletion)
	}

	key, err := r.client.BuildKey("PK", settingsPK(accountID), "SK", "SETTINGS")
	if err != nil {
		return fmt.Errorf("failed to build settings key for restore: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	exprValues := map[string]ddbTypes.AttributeValue{
		":st":         &ddbTypes.AttributeValueMemberS{Value: "active"},
		":ua":         &ddbTypes.AttributeValueMemberS{Value: now},
		":pendingDel": &ddbTypes.AttributeValueMemberS{Value: "pending_deletion"},
	}
	exprNames := map[string]string{
		"#st":   "status",
		"#ua":   "updated_at",
		"#sr":   "status_reason",
		"#scat": "status_changed_at",
	}
	updateExpr := "SET #st = :st, #ua = :ua REMOVE #sr, #scat"
	condition := "attribute_exists(SK) AND #st = :pendingDel"

	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("account not eligible for restore: %w", ErrAccountNotPendingDeletion)
		}
		return fmt.Errorf("failed to restore account: %w", err)
	}
	return nil
}
