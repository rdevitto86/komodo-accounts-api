package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	openapiTypes "github.com/oapi-codegen/runtime/types"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

const (
	emailReservationSK            = "EMAIL"
	emailReservationTransactIndex = 2
	erasureChunkSize              = 100
)

var errUnreadableProfileEmail = errors.New("failed to read the profile email required for erasure")

type erasurePlan struct {
	children []dynamodb.TransactItem
	identity []dynamodb.TransactItem
}

var (
	ErrNotFound                   = dynamodb.ErrNotFound
	ErrAlreadyExists              = errors.New("already exists")
	ErrEmailAlreadyExists         = errors.New("email already registered")
	ErrPasskeyAlreadyExists       = errors.New("passkey already exists")
	ErrPasskeySignCountRegression = errors.New("passkey sign count regression")
	ErrVersionConflict            = errors.New("version conflict")
)

type Repo struct {
	client dynamodb.API
	table  string
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

type credentialsBatchItem struct {
	SK            string   `dynamodbav:"SK"`
	AccountID     string   `dynamodbav:"account_id"`
	PasswordHash  string   `dynamodbav:"password_hash"`
	AuthMethods   []string `dynamodbav:"auth_methods"`
	EmailVerified bool     `dynamodbav:"email_verified"`
	Status        string   `dynamodbav:"status"`
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

type emailReservationRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	AccountID string `dynamodbav:"account_id"`
	Email     string `dynamodbav:"email"`
}

type gsiEmailResult struct {
	AccountID string `dynamodbav:"account_id"`
	PK        string `dynamodbav:"PK"`
}

func New(client dynamodb.API, table string) *Repo {
	return &Repo{client: client, table: table}
}

// DB method that retrieves an account by its ID.
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

// DB method that retrieves account credentials by email.
func (r *Repo) GetAccountCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	accountID, err := r.resolveAccountIDByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to get account credentials: %w", err)
	}

	keys := []map[string]ddbTypes.AttributeValue{
		{
			"PK": &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
			"SK": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
		},
		{
			"PK": &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
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
	settingsFound := false

	for _, item := range items {
		switch item.SK {
		case "PROFILE":
			profile = item
			profileFound = true
		case "SETTINGS":
			settings = item
			settingsFound = true
		}
	}

	if !profileFound {
		return nil, fmt.Errorf("account not found: %w", ErrNotFound)
	}
	if !settingsFound {
		logger.Warn(
			"defaulted credentials status for account missing settings record",
			logger.Attr("account_id", profile.AccountID),
		)
	}

	status := settings.Status
	if status == "" {
		status = "active"
	}

	return &models.CredentialsResponse{
		AccountId:     profile.AccountID,
		PasswordHash:  optStr(profile.PasswordHash),
		EmailVerified: settings.EmailVerified,
		AuthMethods:   toAuthMethods[models.CredentialsResponseAuthMethods](profile.AuthMethods),
		Status:        models.CredentialsResponseStatus(status),
	}, nil
}

// DB method that updates account credentials.
func (r *Repo) UpdateAccountCredentials(ctx context.Context, accountID string, req *models.UpdateCredentialsRequest) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", "PROFILE")
	if err != nil {
		return fmt.Errorf("failed to build credentials key: %w", err)
	}

	setClauses := []string{}
	exprValues := map[string]ddbTypes.AttributeValue{}
	exprNames := map[string]string{}

	if req.PasswordHash != nil {
		setClauses = append(setClauses, "#ph = :ph")
		exprValues[":ph"] = &ddbTypes.AttributeValueMemberS{Value: *req.PasswordHash}
		exprNames["#ph"] = "password_hash"
	}

	if req.AuthMethods != nil {
		methods := derefSlice(req.AuthMethods)
		amList := make([]ddbTypes.AttributeValue, len(methods))
		for i, m := range methods {
			amList[i] = &ddbTypes.AttributeValueMemberS{Value: string(m)}
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

// DB method that checks if an account exists by email.
func (r *Repo) GetAccountExistsByEmail(ctx context.Context, email string) (*models.AccountExistsResponse, error) {
	if _, err := r.resolveAccountIDByEmail(ctx, email); err != nil {
		if errors.Is(err, ErrNotFound) {
			return &models.AccountExistsResponse{Exists: false}, nil
		}
		return nil, fmt.Errorf("failed to check account exists: %w", err)
	}
	return &models.AccountExistsResponse{Exists: true}, nil
}

// DB method that creates a new account.
func (r *Repo) CreateAccount(ctx context.Context, accountID string, req *models.CreateAccountRequest) (*models.Account, error) {
	now := time.Now().UTC()
	email := strings.ToLower(string(req.Email))
	pk := createPK(accountID)

	profileRec := accountRecord{
		PK:          pk,
		SK:          "PROFILE",
		GSI1PK:      "EMAIL#" + email,
		GSI1SK:      "PROFILE",
		AccountID:   accountID,
		Username:    req.Username,
		Email:       email,
		Phone:       deref(req.Phone),
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		AvatarURL:   deref(req.AvatarUrl),
		AuthMethods: []string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	settingsRec := settingsDefaultRecord{
		PK:        pk,
		SK:        "SETTINGS",
		Status:    "active",
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	emailRec := emailReservationRecord{
		PK:        emailReservationPK(email),
		SK:        emailReservationSK,
		AccountID: accountID,
		Email:     email,
	}

	condition := aws.String("attribute_not_exists(SK)")
	err := r.client.TransactWrite(ctx, []dynamodb.TransactItem{
		{Table: r.table, Op: dynamodb.TransactPut, Item: profileRec, Condition: condition},
		{Table: r.table, Op: dynamodb.TransactPut, Item: settingsRec, Condition: condition},
		{Table: r.table, Op: dynamodb.TransactPut, Item: emailRec, Condition: aws.String("attribute_not_exists(PK)")},
	})
	if err != nil {
		if idx, ok := dynamodb.ConditionFailureIndex(err); ok {
			if idx == emailReservationTransactIndex {
				return nil, fmt.Errorf("failed to create account: %w", ErrEmailAlreadyExists)
			}
			return nil, fmt.Errorf("failed to create account: %w", ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to create account: %w", err)
	}
	return profileRec.toModel(), nil
}

// DB method that updates an existing account.
func (r *Repo) UpdateAccount(ctx context.Context, accountID string, update *models.UpdateAccountRequest) (*models.Account, error) {
	pk := createPK(accountID)
	key, err := r.client.BuildKey("PK", pk, "SK", "PROFILE")
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
	if update.AvatarUrl != nil {
		setClauses = append(setClauses, "#av = :av")
		exprValues[":av"] = &ddbTypes.AttributeValueMemberS{Value: *update.AvatarUrl}
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

// DB method that deletes an existing account.
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
		return fmt.Errorf("account not found: %w", ErrNotFound)
	}

	if len(items) > erasureChunkSize {
		logger.Warn("large account delete; processing in chunks",
			logger.Attr("account_id", accountID),
			logger.Attr("item_count", len(items)),
		)
	}

	plan, err := r.buildErasurePlan(items)
	if err != nil {
		return err
	}

	for start := 0; start < len(plan.children); start += erasureChunkSize {
		end := min(start+erasureChunkSize, len(plan.children))
		if err := r.client.TransactWrite(ctx, plan.children[start:end]); err != nil {
			return fmt.Errorf("failed to delete account records: %w", err)
		}
	}
	if err := r.client.TransactWrite(ctx, plan.identity); err != nil {
		return fmt.Errorf("failed to delete account identity records: %w", err)
	}
	return nil
}

func (r *Repo) buildErasurePlan(items []map[string]ddbTypes.AttributeValue) (*erasurePlan, error) {
	plan := &erasurePlan{
		children: make([]dynamodb.TransactItem, 0, len(items)),
		identity: make([]dynamodb.TransactItem, 0, 2),
	}

	for _, item := range items {
		pk, hasPK := item["PK"]
		sk, hasSK := item["SK"]
		if !hasPK || !hasSK {
			continue
		}
		del := dynamodb.TransactItem{
			Table: r.table,
			Op:    dynamodb.TransactDelete,
			Key: map[string]ddbTypes.AttributeValue{
				"PK": pk,
				"SK": sk,
			},
		}

		skVal, isStr := sk.(*ddbTypes.AttributeValueMemberS)
		if !isStr || skVal.Value != "PROFILE" {
			plan.children = append(plan.children, del)
			continue
		}

		emailVal, isEmailStr := item["email"].(*ddbTypes.AttributeValueMemberS)
		if !isEmailStr || emailVal.Value == "" {
			return nil, errUnreadableProfileEmail
		}
		plan.identity = append(plan.identity, del, dynamodb.TransactItem{
			Table: r.table,
			Op:    dynamodb.TransactDelete,
			Key: map[string]ddbTypes.AttributeValue{
				"PK": &ddbTypes.AttributeValueMemberS{Value: emailReservationPK(emailVal.Value)},
				"SK": &ddbTypes.AttributeValueMemberS{Value: emailReservationSK},
			},
		})
	}

	if len(plan.identity) == 0 {
		return nil, fmt.Errorf("failed to find the profile record required for erasure: %w", ErrNotFound)
	}
	return plan, nil
}

// Helper that queries the GSI index to resolve an account ID by email.
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
	if len(results) > 1 {
		ids := make([]string, len(results))
		for i, res := range results {
			ids[i] = res.AccountID
		}
		logger.Error(
			"detected duplicate accounts for one email",
			ErrEmailAlreadyExists,
			logger.Attr("account_ids", ids),
		)
		return "", fmt.Errorf("refusing ambiguous email lookup: %w", ErrEmailAlreadyExists)
	}
	return results[0].AccountID, nil
}

// Helper that converts an account record to a model.
func (r *accountRecord) toModel() *models.Account {
	return &models.Account{
		AccountId: r.AccountID,
		Username:  r.Username,
		Email:     openapiTypes.Email(r.Email),
		Phone:     optStr(r.Phone),
		FirstName: r.FirstName,
		LastName:  r.LastName,
		AvatarUrl: optStr(r.AvatarURL),
	}
}

func emailReservationPK(email string) string { return "EMAIL#" + strings.ToLower(email) }
