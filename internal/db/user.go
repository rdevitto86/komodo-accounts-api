package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"

	"komodo-user-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
)

var ErrNotFound = dynamodb.ErrNotFound
var ErrAlreadyExists = errors.New("already exists")

type Repo struct {
	client *dynamodb.Client
	table  string
}

func New(client *dynamodb.Client, table string) *Repo {
	return &Repo{client: client, table: table}
}

type userRecord struct {
	PK            string    `dynamodbav:"PK"`
	SK            string    `dynamodbav:"SK"`
	UserID        string    `dynamodbav:"user_id"`
	Username      string    `dynamodbav:"username"`
	Email         string    `dynamodbav:"email"`
	Phone         string    `dynamodbav:"phone"`
	FirstName     string    `dynamodbav:"first_name"`
	MiddleInitial string    `dynamodbav:"middle_initial"`
	LastName      string    `dynamodbav:"last_name"`
	AvatarURL     string    `dynamodbav:"avatar_url"`
	PasswordHash  string    `dynamodbav:"password_hash"`
	EmailVerified bool      `dynamodbav:"email_verified"`
	AuthMethods   []string  `dynamodbav:"auth_methods"`
	GSI1PK        string    `dynamodbav:"GSI1PK"`
	GSI1SK        string    `dynamodbav:"GSI1SK"`
	CreatedAt     time.Time `dynamodbav:"created_at"`
	UpdatedAt     time.Time `dynamodbav:"updated_at"`
}

func (r *userRecord) toModel() *models.User {
	return &models.User{
		UserID:        r.UserID,
		Username:      r.Username,
		Email:         r.Email,
		Phone:         r.Phone,
		FirstName:     r.FirstName,
		MiddleInitial: r.MiddleInitial,
		LastName:      r.LastName,
		AvatarURL:     r.AvatarURL,
		EmailVerified: r.EmailVerified,
		AuthMethods:   r.AuthMethods,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func (r *Repo) GetUser(ctx context.Context, userID string) (*models.User, error) {
	key, err := r.client.BuildKey("PK", "USER#"+userID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("repo.GetUser: build key: %w", err)
	}

	var record userRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("repo.GetUser: %w", err)
	}
	return record.toModel(), nil
}

type gsiEmailResult struct {
	UserID string `dynamodbav:"user_id"`
	PK     string `dynamodbav:"PK"`
}

func (r *Repo) getUserByEmail(ctx context.Context, email string) (*userRecord, error) {
	gsiName := "GSI1"
	var results []gsiEmailResult
	if err := r.client.QueryAllAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		IndexName:              &gsiName,
		KeyConditionExpression: "GSI1PK = :pk AND GSI1SK = :sk",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk": &ddbTypes.AttributeValueMemberS{Value: "EMAIL#" + strings.ToLower(email)},
			":sk": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
		},
	}, &results); err != nil {
		return nil, fmt.Errorf("repo.getUserByEmail: GSI query: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("repo.getUserByEmail: %w", ErrNotFound)
	}

	key, err := r.client.BuildKey("PK", "USER#"+results[0].UserID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("repo.getUserByEmail: build key: %w", err)
	}
	var record userRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("repo.getUserByEmail: get item: %w", err)
	}
	return &record, nil
}

func (r *Repo) GetUserCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	record, err := r.getUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("repo.GetUserCredentialsByEmail: %w", err)
	}
	return &models.CredentialsResponse{
		UserID:        record.UserID,
		PasswordHash:  record.PasswordHash,
		EmailVerified: record.EmailVerified,
		AuthMethods:   record.AuthMethods,
	}, nil
}

func (r *Repo) UpdateUserCredentials(ctx context.Context, userID string, req *models.UpdateCredentialsRequest) error {
	key, err := r.client.BuildKey("PK", "USER#"+userID, "SK", "PROFILE")
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

func (r *Repo) GetUserExistsByEmail(ctx context.Context, email string) (*models.UserExistsResponse, error) {
	record, err := r.getUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return &models.UserExistsResponse{Exists: false, AuthMethods: []string{}}, nil
		}
		return nil, fmt.Errorf("repo.GetUserExistsByEmail: %w", err)
	}
	return &models.UserExistsResponse{
		Exists:      true,
		AuthMethods: record.AuthMethods,
	}, nil
}

func (r *Repo) CreateUser(ctx context.Context, user *models.User) error {
	record := userRecord{
		PK:            "USER#" + user.UserID,
		SK:            "PROFILE",
		GSI1PK:        "EMAIL#" + strings.ToLower(user.Email),
		GSI1SK:        "PROFILE",
		UserID:        user.UserID,
		Username:      user.Username,
		Email:         strings.ToLower(user.Email),
		Phone:         user.Phone,
		FirstName:     user.FirstName,
		MiddleInitial: user.MiddleInitial,
		LastName:      user.LastName,
		AvatarURL:     user.AvatarURL,
		PasswordHash:  user.PasswordHash,
		EmailVerified: user.EmailVerified,
		AuthMethods:   user.AuthMethods,
		CreatedAt:     user.CreatedAt,
		UpdatedAt:     user.UpdatedAt,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("repo.CreateUser: %w", err)
	}
	return nil
}

func (r *Repo) UpdateUser(ctx context.Context, userID string, update *models.User) (*models.User, error) {
	key, err := r.client.BuildKey("PK", "USER#"+userID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("repo.UpdateUser: build key: %w", err)
	}

	var existing userRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &existing); err != nil {
		return nil, fmt.Errorf("repo.UpdateUser: get existing: %w", err)
	}

	if update.Phone != "" {
		existing.Phone = update.Phone
	}
	if update.FirstName != "" {
		existing.FirstName = update.FirstName
	}
	if update.MiddleInitial != "" {
		existing.MiddleInitial = update.MiddleInitial
	}
	if update.LastName != "" {
		existing.LastName = update.LastName
	}
	if update.AvatarURL != "" {
		existing.AvatarURL = update.AvatarURL
	}
	existing.UpdatedAt = update.UpdatedAt

	if err := r.client.WriteItemFrom(ctx, r.table, existing, false, nil, nil); err != nil {
		return nil, fmt.Errorf("repo.UpdateUser: write: %w", err)
	}
	return existing.toModel(), nil
}

func (r *Repo) DeleteUser(ctx context.Context, userID string) error {
	items, err := r.client.QueryAll(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk": &ddbTypes.AttributeValueMemberS{Value: "USER#" + userID},
		},
	})
	if err != nil {
		return fmt.Errorf("repo.DeleteUser: query user items: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	keys := make([]map[string]ddbTypes.AttributeValue, 0, len(items))
	for _, item := range items {
		pk, hasPK := item["PK"]
		sk, hasSK := item["SK"]
		if !hasPK || !hasSK {
			continue
		}
		keys = append(keys, map[string]ddbTypes.AttributeValue{"PK": pk, "SK": sk})
	}

	if err := r.client.DeleteItem(ctx, r.table, nil, true, keys, nil); err != nil {
		return fmt.Errorf("repo.DeleteUser: batch delete: %w", err)
	}
	return nil
}

type addressRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Address
}

func addrPK(userID string) string { return "USER#" + userID }

func addrSK(addressID string) string { return "ADDR#" + addressID }

func (r *Repo) CreateAddress(ctx context.Context, userID string, addr *models.Address) error {
	if addr.AddressID == "" {
		raw := strings.ReplaceAll(uuid.NewString(), "-", "")
		addr.AddressID = "addr_" + raw[:12]
	}

	record := addressRecord{
		PK:      addrPK(userID),
		SK:      addrSK(addr.AddressID),
		Address: *addr,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("repo.CreateAddress: %w", err)
	}
	return nil
}

func (r *Repo) GetAddress(ctx context.Context, userID, addressID string) (*models.Address, error) {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
	if err != nil {
		return nil, fmt.Errorf("repo.GetAddress: build key: %w", err)
	}

	var record addressRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("repo.GetAddress: %w", err)
	}
	addr := record.Address
	return &addr, nil
}

func (r *Repo) GetUserAddresses(ctx context.Context, userID string) ([]models.Address, error) {
	var records []addressRecord
	if err := r.client.QueryAllAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: addrPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "ADDR#"},
		},
	}, &records); err != nil {
		return nil, fmt.Errorf("repo.GetUserAddresses: %w", err)
	}

	addrs := make([]models.Address, len(records))
	for i, r := range records {
		addrs[i] = r.Address
	}
	return addrs, nil
}

func (r *Repo) UpdateAddress(ctx context.Context, userID string, addr models.Address) error {
	if addr.AddressID == "" {
		return fmt.Errorf("repo.UpdateAddress: address_id is required")
	}

	record := addressRecord{
		PK:      addrPK(userID),
		SK:      addrSK(addr.AddressID),
		Address: addr,
	}
	condition := "attribute_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("repo.UpdateAddress: %w", ErrNotFound)
		}
		return fmt.Errorf("repo.UpdateAddress: %w", err)
	}
	return nil
}

func (r *Repo) DeleteAddress(ctx context.Context, userID, addressID string) error {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("repo.DeleteAddress: build key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("repo.DeleteAddress: %w", err)
	}
	return nil
}

func (r *Repo) SetAddressDefault(ctx context.Context, userID, addressID string, isDefault bool) error {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("repo.SetAddressDefault: build key: %w", err)
	}
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key,
		"SET #isDefault = :v",
		map[string]ddbTypes.AttributeValue{":v": &ddbTypes.AttributeValueMemberBOOL{Value: isDefault}},
		map[string]string{"#isDefault": "is_default"},
		&condition,
	); err != nil {
		return fmt.Errorf("repo.SetAddressDefault: %w", err)
	}
	return nil
}

type paymentRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.PaymentMethod
}

func payPK(userID string) string { return "USER#" + userID }

func paySK(paymentID string) string { return "PAY#" + paymentID }

func (r *Repo) UpsertPayment(ctx context.Context, userID string, method *models.PaymentMethod) error {
	if method.PaymentID == "" {
		raw := strings.ReplaceAll(uuid.NewString(), "-", "")
		method.PaymentID = "pay_" + raw[:12]
	}

	record := paymentRecord{
		PK:            payPK(userID),
		SK:            paySK(method.PaymentID),
		PaymentMethod: *method,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("repo.UpsertPayment: %w", err)
	}
	return nil
}

func (r *Repo) GetPayment(ctx context.Context, userID, paymentID string) (*models.PaymentMethod, error) {
	key, err := r.client.BuildKey("PK", payPK(userID), "SK", paySK(paymentID))
	if err != nil {
		return nil, fmt.Errorf("repo.GetPayment: build key: %w", err)
	}

	var record paymentRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("repo.GetPayment: %w", err)
	}

	pm := record.PaymentMethod
	pm.Token = ""
	return &pm, nil
}

func (r *Repo) ListPayments(ctx context.Context, userID string) ([]models.PaymentMethod, error) {
	var records []paymentRecord
	if err := r.client.QueryAllAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: payPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PAY#"},
		},
	}, &records); err != nil {
		return nil, fmt.Errorf("repo.ListPayments: %w", err)
	}

	methods := make([]models.PaymentMethod, len(records))
	for i, r := range records {
		pm := r.PaymentMethod
		pm.Token = ""
		methods[i] = pm
	}
	return methods, nil
}

func (r *Repo) DeletePayment(ctx context.Context, userID, paymentID string) error {
	key, err := r.client.BuildKey("PK", payPK(userID), "SK", paySK(paymentID))
	if err != nil {
		return fmt.Errorf("repo.DeletePayment: build key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("repo.DeletePayment: %w", err)
	}
	return nil
}

func (r *Repo) SetPaymentDefault(ctx context.Context, userID, paymentID string, isDefault bool) error {
	key, err := r.client.BuildKey("PK", payPK(userID), "SK", paySK(paymentID))
	if err != nil {
		return fmt.Errorf("repo.SetPaymentDefault: build key: %w", err)
	}
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key,
		"SET #isDefault = :v",
		map[string]ddbTypes.AttributeValue{":v": &ddbTypes.AttributeValueMemberBOOL{Value: isDefault}},
		map[string]string{"#isDefault": "is_default"},
		&condition,
	); err != nil {
		return fmt.Errorf("repo.SetPaymentDefault: %w", err)
	}
	return nil
}

type prefsRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Preferences
}

func prefsPK(userID string) string { return "USER#" + userID }

func (r *Repo) GetUserPreferences(ctx context.Context, userID string) (*models.Preferences, error) {
	key, err := r.client.BuildKey("PK", prefsPK(userID), "SK", "PREFS")
	if err != nil {
		return nil, fmt.Errorf("repo.GetUserPreferences: build key: %w", err)
	}

	var record prefsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("repo.GetUserPreferences: %w", err)
	}
	prefs := record.Preferences
	return &prefs, nil
}

func (r *Repo) UpdateUserPreferences(ctx context.Context, userID string, prefs *models.Preferences) error {
	record := prefsRecord{
		PK:          prefsPK(userID),
		SK:          "PREFS",
		Preferences: *prefs,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("repo.UpdateUserPreferences: %w", err)
	}
	return nil
}

func (r *Repo) DeleteUserPreferences(ctx context.Context, userID string) error {
	key, err := r.client.BuildKey("PK", prefsPK(userID), "SK", "PREFS")
	if err != nil {
		return fmt.Errorf("repo.DeleteUserPreferences: build key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("repo.DeleteUserPreferences: %w", err)
	}
	return nil
}

type passkeyRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.PasskeyCredential
}

func passkeyPK(userID string) string { return "USER#" + userID }

func passkeySK(credentialID string) string { return "PASSKEY#" + credentialID }

func (r *Repo) CreatePasskey(ctx context.Context, userID string, cred *models.PasskeyCredential) error {
	if cred.CredentialID == "" {
		return fmt.Errorf("credential_id is required")
	}

	now := time.Now().UTC()
	cred.CreatedAt = now
	cred.LastUsedAt = nil

	record := passkeyRecord{
		PK:                passkeyPK(userID),
		SK:                passkeySK(cred.CredentialID),
		PasskeyCredential: *cred,
	}

	condition := "attribute_not_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("failed to create passkey: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to write passkey: %w", err)
	}
	return nil
}

func (r *Repo) GetUserPasskeys(ctx context.Context, userID string) ([]models.PasskeyCredential, error) {
	var records []passkeyRecord
	if err := r.client.QueryAllAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: passkeyPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PASSKEY#"},
		},
	}, &records); err != nil {
		return nil, fmt.Errorf("failed to query passkeys: %w", err)
	}

	creds := make([]models.PasskeyCredential, len(records))
	for i, r := range records {
		creds[i] = r.PasskeyCredential
	}
	return creds, nil
}

func (r *Repo) UpdatePasskey(ctx context.Context, userID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error) {
	key, err := r.client.BuildKey("PK", passkeyPK(userID), "SK", passkeySK(credentialID))
	if err != nil {
		return nil, fmt.Errorf("failed to build passkey key: %w", err)
	}

	var existing passkeyRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &existing); err != nil {
		return nil, fmt.Errorf("failed to get existing passkey: %w", err)
	}

	existing.SignCount = update.SignCount
	if update.LastUsedAt != nil {
		existing.LastUsedAt = update.LastUsedAt
	}
	if update.BackupState != existing.BackupState {
		existing.BackupState = update.BackupState
	}

	if err := r.client.WriteItemFrom(ctx, r.table, existing, false, nil, nil); err != nil {
		return nil, fmt.Errorf("failed to write passkey: %w", err)
	}
	cred := existing.PasskeyCredential
	return &cred, nil
}

func (r *Repo) DeletePasskey(ctx context.Context, userID, credentialID string) error {
	key, err := r.client.BuildKey("PK", passkeyPK(userID), "SK", passkeySK(credentialID))
	if err != nil {
		return fmt.Errorf("failed to build passkey key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}
