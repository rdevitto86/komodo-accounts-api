package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/segmentio/ksuid"

	"komodo-customer-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

var ErrNotFound = dynamodb.ErrNotFound
var ErrAlreadyExists = errors.New("already exists")
var ErrPasskeyAlreadyExists = errors.New("passkey already exists")
var ErrPasskeySignCountRegression = errors.New("passkey sign count regression")
var ErrAccountNotPendingDeletion = errors.New("account not pending deletion")
var ErrVersionConflict = errors.New("version conflict")

type ddbRawAPI interface {
	TransactWriteItems(ctx context.Context, params *awsdynamodb.TransactWriteItemsInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.TransactWriteItemsOutput, error)
	BatchGetItem(ctx context.Context, params *awsdynamodb.BatchGetItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.BatchGetItemOutput, error)
}

type Repo struct {
	client    *dynamodb.Client
	rawClient ddbRawAPI
	table     string
}

func New(client *dynamodb.Client, rawClient ddbRawAPI, table string) *Repo {
	return &Repo{client: client, rawClient: rawClient, table: table}
}

type customerRecord struct {
	PK            string    `dynamodbav:"PK"`
	SK            string    `dynamodbav:"SK"`
	CustomerID    string    `dynamodbav:"customer_id"`
	Username      string    `dynamodbav:"username"`
	Email         string    `dynamodbav:"email"`
	Phone         string    `dynamodbav:"phone"`
	FirstName     string    `dynamodbav:"first_name"`
	LastName      string    `dynamodbav:"last_name"`
	AvatarURL     string    `dynamodbav:"avatar_url"`
	PasswordHash  string    `dynamodbav:"password_hash"`
	AuthMethods   []string  `dynamodbav:"auth_methods"`
	GSI1PK        string    `dynamodbav:"GSI1PK"`
	GSI1SK        string    `dynamodbav:"GSI1SK"`
	CreatedAt     time.Time `dynamodbav:"created_at"`
	UpdatedAt     time.Time `dynamodbav:"updated_at"`
}

func (r *customerRecord) toModel() *models.User {
	return &models.User{
		CustomerID:    r.CustomerID,
		Username:      r.Username,
		Email:         r.Email,
		Phone:         r.Phone,
		FirstName:     r.FirstName,
		LastName:      r.LastName,
		AvatarURL:     r.AvatarURL,
		AuthMethods:   r.AuthMethods,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func (r *Repo) GetUser(ctx context.Context, userID string) (*models.User, error) {
	key, err := r.client.BuildKey("PK", "CUSTOMER#"+userID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build user key: %w", err)
	}

	var record customerRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return record.toModel(), nil
}

type gsiEmailResult struct {
	CustomerID string `dynamodbav:"customer_id"`
	PK         string `dynamodbav:"PK"`
}

func (r *Repo) resolveCustomerIDByEmail(ctx context.Context, email string) (string, error) {
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
		return "", fmt.Errorf("failed to query user by email: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("user not found: %w", ErrNotFound)
	}
	return results[0].CustomerID, nil
}

func (r *Repo) getUserByEmail(ctx context.Context, email string) (*customerRecord, error) {
	customerID, err := r.resolveCustomerIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	key, err := r.client.BuildKey("PK", "CUSTOMER#"+customerID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build email lookup key: %w", err)
	}
	var record customerRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &record, nil
}

func (r *Repo) GetUserCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	customerID, err := r.resolveCustomerIDByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}

	pk := "CUSTOMER#" + customerID
	batchOut, err := r.rawClient.BatchGetItem(ctx, &awsdynamodb.BatchGetItemInput{
		RequestItems: map[string]ddbTypes.KeysAndAttributes{
			r.table: {
				Keys: []map[string]ddbTypes.AttributeValue{
					{
						"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
						"SK": &ddbTypes.AttributeValueMemberS{Value: "PROFILE"},
					},
					{
						"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
						"SK": &ddbTypes.AttributeValueMemberS{Value: "SETTINGS"},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to batch get credentials: %w", err)
	}

	var profile customerRecord
	var settings settingsRecord
	profileFound := false

	for _, item := range batchOut.Responses[r.table] {
		skAttr, ok := item["SK"]
		if !ok {
			continue
		}
		skVal, ok := skAttr.(*ddbTypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		switch skVal.Value {
		case "PROFILE":
			if err := attributevalue.UnmarshalMap(item, &profile); err != nil {
				return nil, fmt.Errorf("failed to unmarshal credentials profile: %w", err)
			}
			profileFound = true
		case "SETTINGS":
			if err := attributevalue.UnmarshalMap(item, &settings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal credentials settings: %w", err)
			}
		}
	}

	if !profileFound {
		return nil, fmt.Errorf("user not found: %w", ErrNotFound)
	}

	return &models.CredentialsResponse{
		CustomerID:    profile.CustomerID,
		PasswordHash:  profile.PasswordHash,
		EmailVerified: settings.EmailVerified,
		AuthMethods:   profile.AuthMethods,
	}, nil
}

func (r *Repo) UpdateUserCredentials(ctx context.Context, userID string, req *models.UpdateCredentialsRequest) error {
	key, err := r.client.BuildKey("PK", "CUSTOMER#"+userID, "SK", "PROFILE")
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
		return nil, fmt.Errorf("failed to check user exists: %w", err)
	}
	return &models.UserExistsResponse{
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
	CreatedAt     time.Time `dynamodbav:"created_at"`
	UpdatedAt     time.Time `dynamodbav:"updated_at"`
}

func (r *Repo) CreateUser(ctx context.Context, user *models.User) error {
	profileRec := customerRecord{
		PK:            "CUSTOMER#" + user.CustomerID,
		SK:            "PROFILE",
		GSI1PK:        "EMAIL#" + strings.ToLower(user.Email),
		GSI1SK:        "PROFILE",
		CustomerID:    user.CustomerID,
		Username:      user.Username,
		Email:         strings.ToLower(user.Email),
		Phone:         user.Phone,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		AvatarURL:     user.AvatarURL,
		PasswordHash:  user.PasswordHash,
		AuthMethods:   user.AuthMethods,
		CreatedAt:     user.CreatedAt,
		UpdatedAt:     user.UpdatedAt,
	}
	profileItem, err := attributevalue.MarshalMap(profileRec)
	if err != nil {
		return fmt.Errorf("failed to marshal profile for create: %w", err)
	}

	settingsRec := settingsDefaultRecord{
		PK:        "CUSTOMER#" + user.CustomerID,
		SK:        "SETTINGS",
		Status:    "active",
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
	settingsItem, err := attributevalue.MarshalMap(settingsRec)
	if err != nil {
		return fmt.Errorf("failed to marshal settings for create: %w", err)
	}
	settingsItem["version"] = &ddbTypes.AttributeValueMemberN{Value: "0"}

	condition := aws.String("attribute_not_exists(SK)")
	_, err = r.rawClient.TransactWriteItems(ctx, &awsdynamodb.TransactWriteItemsInput{
		TransactItems: []ddbTypes.TransactWriteItem{
			{
				Put: &ddbTypes.Put{
					TableName:           aws.String(r.table),
					Item:                profileItem,
					ConditionExpression: condition,
				},
			},
			{
				Put: &ddbTypes.Put{
					TableName:           aws.String(r.table),
					Item:                settingsItem,
					ConditionExpression: condition,
				},
			},
		},
	})
	if err != nil {
		var txErr *ddbTypes.TransactionCanceledException
		if errors.As(err, &txErr) {
			return fmt.Errorf("failed to create user: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func (r *Repo) UpdateUser(ctx context.Context, userID string, update *models.User) (*models.User, error) {
	key, err := r.client.BuildKey("PK", "CUSTOMER#"+userID, "SK", "PROFILE")
	if err != nil {
		return nil, fmt.Errorf("failed to build update key: %w", err)
	}

	var existing customerRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &existing); err != nil {
		return nil, fmt.Errorf("failed to get existing user: %w", err)
	}

	if update.Phone != "" {
		existing.Phone = update.Phone
	}
	if update.FirstName != "" {
		existing.FirstName = update.FirstName
	}
	if update.LastName != "" {
		existing.LastName = update.LastName
	}
	if update.AvatarURL != "" {
		existing.AvatarURL = update.AvatarURL
	}
	existing.UpdatedAt = update.UpdatedAt

	if err := r.client.WriteItemFrom(ctx, r.table, existing, false, nil, nil); err != nil {
		return nil, fmt.Errorf("failed to write user update: %w", err)
	}
	return existing.toModel(), nil
}

func (r *Repo) DeleteUser(ctx context.Context, userID string) error {
	items, err := r.client.QueryAll(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk": &ddbTypes.AttributeValueMemberS{Value: "CUSTOMER#" + userID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query user items: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	if len(items) > 100 {
		logger.Warn("large user delete; processing in chunks",
			logger.Attr("customer_id", userID),
			logger.Attr("item_count", len(items)),
		)
	}

	const chunkSize = 100
	for start := 0; start < len(items); start += chunkSize {
		end := start + chunkSize
		if end > len(items) {
			end = len(items)
		}

		transactItems := make([]ddbTypes.TransactWriteItem, 0, end-start)
		for _, item := range items[start:end] {
			pk, hasPK := item["PK"]
			sk, hasSK := item["SK"]
			if !hasPK || !hasSK {
				continue
			}
			transactItems = append(transactItems, ddbTypes.TransactWriteItem{
				Delete: &ddbTypes.Delete{
					TableName: aws.String(r.table),
					Key: map[string]ddbTypes.AttributeValue{
						"PK": pk,
						"SK": sk,
					},
				},
			})
		}

		if len(transactItems) == 0 {
			continue
		}

		if _, err := r.rawClient.TransactWriteItems(ctx, &awsdynamodb.TransactWriteItemsInput{
			TransactItems: transactItems,
		}); err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
	}
	return nil
}

type addressRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Address
}

func addrPK(userID string) string { return "CUSTOMER#" + userID }

func addrSK(addressID string) string { return "ADDR#" + addressID }

func (r *Repo) CreateAddress(ctx context.Context, userID string, addr *models.Address) error {
	if addr.AddressID == "" {
		addr.AddressID = "addr_" + ksuid.New().String()
	}

	record := addressRecord{
		PK:      addrPK(userID),
		SK:      addrSK(addr.AddressID),
		Address: *addr,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("failed to create address: %w", err)
	}
	return nil
}

func (r *Repo) GetAddress(ctx context.Context, userID, addressID string) (*models.Address, error) {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
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

func (r *Repo) GetUserAddresses(ctx context.Context, userID string) ([]models.Address, error) {
	var records []addressRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: addrPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "ADDR#"},
		},
		Limit: aws.Int32(100),
	}, &records); err != nil {
		return nil, fmt.Errorf("failed to get user addresses: %w", err)
	}

	addrs := make([]models.Address, len(records))
	for i, r := range records {
		addrs[i] = r.Address
	}
	return addrs, nil
}

func (r *Repo) UpdateAddress(ctx context.Context, userID, addressID string, req *models.UpdateAddressRequest) error {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
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

func (r *Repo) DeleteAddress(ctx context.Context, userID, addressID string) error {
	key, err := r.client.BuildKey("PK", addrPK(userID), "SK", addrSK(addressID))
	if err != nil {
		return fmt.Errorf("failed to build delete address key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}

func (r *Repo) SetAddressDefault(ctx context.Context, userID, addressID string) error {
	pk := addrPK(userID)
	targetSK := addrSK(addressID)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	type addrMinRecord struct {
		PK        string `dynamodbav:"PK"`
		SK        string `dynamodbav:"SK"`
		IsDefault bool   `dynamodbav:"is_default"`
	}

	filterExpr := aws.String("#isd = :true AND SK <> :targetSK")
	var current []addrMinRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		FilterExpression:       filterExpr,
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: pk},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "ADDR#"},
			":true":     &ddbTypes.AttributeValueMemberBOOL{Value: true},
			":targetSK": &ddbTypes.AttributeValueMemberS{Value: targetSK},
		},
		ExpressionNames: map[string]string{"#isd": "is_default"},
		Limit:           aws.Int32(1),
	}, &current); err != nil {
		return fmt.Errorf("failed to query current default address: %w", err)
	}

	if len(current) > 0 {
		oldSK := current[0].SK
		_, err := r.rawClient.TransactWriteItems(ctx, &awsdynamodb.TransactWriteItemsInput{
			TransactItems: []ddbTypes.TransactWriteItem{
				{
					Update: &ddbTypes.Update{
						TableName: aws.String(r.table),
						Key: map[string]ddbTypes.AttributeValue{
							"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
							"SK": &ddbTypes.AttributeValueMemberS{Value: oldSK},
						},
						UpdateExpression:    aws.String("SET #isd = :false, #ua = :now"),
						ConditionExpression: aws.String("attribute_exists(SK)"),
						ExpressionAttributeNames: map[string]string{
							"#isd": "is_default",
							"#ua":  "updated_at",
						},
						ExpressionAttributeValues: map[string]ddbTypes.AttributeValue{
							":false": &ddbTypes.AttributeValueMemberBOOL{Value: false},
							":now":   &ddbTypes.AttributeValueMemberS{Value: now},
						},
					},
				},
				{
					Update: &ddbTypes.Update{
						TableName: aws.String(r.table),
						Key: map[string]ddbTypes.AttributeValue{
							"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
							"SK": &ddbTypes.AttributeValueMemberS{Value: targetSK},
						},
						UpdateExpression:    aws.String("SET #isd = :true, #ua = :now"),
						ConditionExpression: aws.String("attribute_exists(SK)"),
						ExpressionAttributeNames: map[string]string{
							"#isd": "is_default",
							"#ua":  "updated_at",
						},
						ExpressionAttributeValues: map[string]ddbTypes.AttributeValue{
							":true": &ddbTypes.AttributeValueMemberBOOL{Value: true},
							":now":  &ddbTypes.AttributeValueMemberS{Value: now},
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to set address default: %w", err)
		}
		return nil
	}

	key, err := r.client.BuildKey("PK", pk, "SK", targetSK)
	if err != nil {
		return fmt.Errorf("failed to build set-default address key: %w", err)
	}
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key,
		"SET #isDefault = :true, #ua = :now",
		map[string]ddbTypes.AttributeValue{
			":true": &ddbTypes.AttributeValueMemberBOOL{Value: true},
			":now":  &ddbTypes.AttributeValueMemberS{Value: now},
		},
		map[string]string{"#isDefault": "is_default", "#ua": "updated_at"},
		&condition,
	); err != nil {
		return fmt.Errorf("failed to set address default: %w", err)
	}
	return nil
}

type paymentRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.PaymentMethod
}

func payPK(userID string) string { return "CUSTOMER#" + userID }

func paySK(paymentID string) string { return "PAY#" + paymentID }

func (r *Repo) UpsertPayment(ctx context.Context, userID string, method *models.PaymentMethod) error {
	if method.PaymentID == "" {
		method.PaymentID = "pay_" + ksuid.New().String()
	}

	record := paymentRecord{
		PK:            payPK(userID),
		SK:            paySK(method.PaymentID),
		PaymentMethod: *method,
	}
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("failed to upsert payment: %w", err)
	}
	return nil
}

func (r *Repo) GetPayment(ctx context.Context, userID, paymentID string) (*models.PaymentMethod, error) {
	key, err := r.client.BuildKey("PK", payPK(userID), "SK", paySK(paymentID))
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

func (r *Repo) ListPayments(ctx context.Context, userID string) ([]models.PaymentMethod, error) {
	var records []paymentRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: payPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PAY#"},
		},
		Limit: aws.Int32(100),
	}, &records); err != nil {
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

func (r *Repo) DeletePayment(ctx context.Context, userID, paymentID string) error {
	key, err := r.client.BuildKey("PK", payPK(userID), "SK", paySK(paymentID))
	if err != nil {
		return fmt.Errorf("failed to build delete payment key: %w", err)
	}
	if err := r.client.DeleteItem(ctx, r.table, key, false, nil, nil); err != nil {
		return fmt.Errorf("failed to delete payment: %w", err)
	}
	return nil
}

func (r *Repo) SetPaymentDefault(ctx context.Context, userID, paymentID string) error {
	pk := payPK(userID)
	targetSK := paySK(paymentID)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	type payMinRecord struct {
		PK        string `dynamodbav:"PK"`
		SK        string `dynamodbav:"SK"`
		IsDefault bool   `dynamodbav:"is_default"`
	}

	filterExpr := aws.String("#isd = :true AND SK <> :targetSK")
	var current []payMinRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		FilterExpression:       filterExpr,
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: pk},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PAY#"},
			":true":     &ddbTypes.AttributeValueMemberBOOL{Value: true},
			":targetSK": &ddbTypes.AttributeValueMemberS{Value: targetSK},
		},
		ExpressionNames: map[string]string{"#isd": "is_default"},
		Limit:           aws.Int32(1),
	}, &current); err != nil {
		return fmt.Errorf("failed to query current default payment: %w", err)
	}

	if len(current) > 0 {
		oldSK := current[0].SK
		_, err := r.rawClient.TransactWriteItems(ctx, &awsdynamodb.TransactWriteItemsInput{
			TransactItems: []ddbTypes.TransactWriteItem{
				{
					Update: &ddbTypes.Update{
						TableName: aws.String(r.table),
						Key: map[string]ddbTypes.AttributeValue{
							"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
							"SK": &ddbTypes.AttributeValueMemberS{Value: oldSK},
						},
						UpdateExpression:    aws.String("SET #isd = :false, #ua = :now"),
						ConditionExpression: aws.String("attribute_exists(SK)"),
						ExpressionAttributeNames: map[string]string{
							"#isd": "is_default",
							"#ua":  "updated_at",
						},
						ExpressionAttributeValues: map[string]ddbTypes.AttributeValue{
							":false": &ddbTypes.AttributeValueMemberBOOL{Value: false},
							":now":   &ddbTypes.AttributeValueMemberS{Value: now},
						},
					},
				},
				{
					Update: &ddbTypes.Update{
						TableName: aws.String(r.table),
						Key: map[string]ddbTypes.AttributeValue{
							"PK": &ddbTypes.AttributeValueMemberS{Value: pk},
							"SK": &ddbTypes.AttributeValueMemberS{Value: targetSK},
						},
						UpdateExpression:    aws.String("SET #isd = :true, #ua = :now"),
						ConditionExpression: aws.String("attribute_exists(SK)"),
						ExpressionAttributeNames: map[string]string{
							"#isd": "is_default",
							"#ua":  "updated_at",
						},
						ExpressionAttributeValues: map[string]ddbTypes.AttributeValue{
							":true": &ddbTypes.AttributeValueMemberBOOL{Value: true},
							":now":  &ddbTypes.AttributeValueMemberS{Value: now},
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to set payment default: %w", err)
		}
		return nil
	}

	key, err := r.client.BuildKey("PK", pk, "SK", targetSK)
	if err != nil {
		return fmt.Errorf("failed to build set-default payment key: %w", err)
	}
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key,
		"SET #isDefault = :true, #ua = :now",
		map[string]ddbTypes.AttributeValue{
			":true": &ddbTypes.AttributeValueMemberBOOL{Value: true},
			":now":  &ddbTypes.AttributeValueMemberS{Value: now},
		},
		map[string]string{"#isDefault": "is_default", "#ua": "updated_at"},
		&condition,
	); err != nil {
		return fmt.Errorf("failed to set payment default: %w", err)
	}
	return nil
}

type prefsRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.Preferences
}

func prefsPK(userID string) string { return "CUSTOMER#" + userID }

func (r *Repo) GetUserPreferences(ctx context.Context, userID string) (*models.Preferences, error) {
	key, err := r.client.BuildKey("PK", prefsPK(userID), "SK", "PREFS")
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

func (r *Repo) UpdateUserPreferences(ctx context.Context, userID string, req *models.UpdatePreferencesRequest) error {
	key, err := r.client.BuildKey("PK", prefsPK(userID), "SK", "PREFS")
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

func (r *Repo) DeleteUserPreferences(ctx context.Context, userID string) error {
	key, err := r.client.BuildKey("PK", prefsPK(userID), "SK", "PREFS")
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

func passkeyPK(userID string) string { return "CUSTOMER#" + userID }

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
			return fmt.Errorf("failed to create passkey: %w", ErrPasskeyAlreadyExists)
		}
		return fmt.Errorf("failed to write passkey: %w", err)
	}
	return nil
}

func (r *Repo) GetUserPasskeys(ctx context.Context, userID string) ([]models.PasskeyCredential, error) {
	var records []passkeyRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: passkeyPK(userID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "PASSKEY#"},
		},
		Limit: aws.Int32(100),
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

type settingsRecord struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	models.AccountSettings
}

func settingsPK(customerID string) string { return "CUSTOMER#" + customerID }

func (r *Repo) GetSettings(ctx context.Context, customerID string) (*models.AccountSettings, error) {
	key, err := r.client.BuildKey("PK", settingsPK(customerID), "SK", "SETTINGS")
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

func (r *Repo) UpdateSettingsPartial(ctx context.Context, customerID string, req *models.UpdateSettingsRequest, version int) error {
	key, err := r.client.BuildKey("PK", settingsPK(customerID), "SK", "SETTINGS")
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
			return fmt.Errorf("version conflict: %w", ErrVersionConflict)
		}
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

func (r *Repo) UpdateSettings(ctx context.Context, customerID string, s *models.AccountSettings) error {
	record := settingsRecord{
		PK:              settingsPK(customerID),
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

func consentPK(customerID string) string { return "CUSTOMER#" + customerID }

func consentSK(channel string, at time.Time) string {
	return "CONSENT#" + channel + "#" + at.UTC().Format(time.RFC3339Nano)
}

func (r *Repo) AppendConsentLog(ctx context.Context, customerID string, entry *models.ConsentLog) error {
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	record := consentRecord{
		PK:         consentPK(customerID),
		SK:         consentSK(entry.Channel, entry.RecordedAt),
		ConsentLog: *entry,
	}
	condition := "attribute_not_exists(SK)"
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, &condition); err != nil {
		return fmt.Errorf("failed to append consent log: %w", err)
	}
	return nil
}

func (r *Repo) ListConsentHistory(ctx context.Context, customerID string) ([]models.ConsentLog, error) {
	var records []consentRecord
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :prefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":     &ddbTypes.AttributeValueMemberS{Value: consentPK(customerID)},
			":prefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#"},
		},
		Limit: aws.Int32(1000),
	}, &records); err != nil {
		return nil, fmt.Errorf("failed to list consent history: %w", err)
	}
	logs := make([]models.ConsentLog, len(records))
	for i, r := range records {
		logs[i] = r.ConsentLog
	}
	return logs, nil
}

func (r *Repo) MutateSettingsTags(ctx context.Context, customerID string, req *models.UpdateSettingsTagsRequest, version int) error {
	if len(req.Add) == 0 && len(req.Remove) == 0 {
		return nil
	}

	key, err := r.client.BuildKey("PK", settingsPK(customerID), "SK", "SETTINGS")
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
			return fmt.Errorf("version conflict: %w", ErrVersionConflict)
		}
		return fmt.Errorf("failed to mutate settings tags: %w", err)
	}
	return nil
}

func (r *Repo) GetLatestConsent(ctx context.Context, customerID, channel string) (*models.ConsentLog, error) {
	var records []consentRecord
	scanForward := false
	if _, err := r.client.QueryAs(ctx, dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: consentPK(customerID)},
			":skPrefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#" + channel + "#"},
		},
		ScanIndexForward: &scanForward,
		Limit:            aws.Int32(1),
	}, &records); err != nil {
		return nil, fmt.Errorf("failed to query consent log: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("consent log not found: %w", ErrNotFound)
	}
	entry := records[0].ConsentLog
	return &entry, nil
}

func (r *Repo) SoftDeleteCustomer(ctx context.Context, customerID string) error {
	key, err := r.client.BuildKey("PK", settingsPK(customerID), "SK", "SETTINGS")
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
	condition := "attribute_exists(SK)"
	if _, err := r.client.UpdateItem(ctx, r.table, key, updateExpr, exprValues, exprNames, &condition); err != nil {
		var conditionalCheckErr *ddbTypes.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckErr) {
			return fmt.Errorf("settings record not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to soft-delete customer: %w", err)
	}
	return nil
}

func (r *Repo) RestoreCustomer(ctx context.Context, customerID string) error {
	settings, err := r.GetSettings(ctx, customerID)
	if err != nil {
		return fmt.Errorf("failed to get settings for restore: %w", err)
	}

	if settings.Status != "pending_deletion" {
		return fmt.Errorf("account not in pending deletion state: %w", ErrAccountNotPendingDeletion)
	}
	if settings.StatusChangedAt == nil || time.Since(*settings.StatusChangedAt) > 30*24*time.Hour {
		return fmt.Errorf("account restore window expired: %w", ErrAccountNotPendingDeletion)
	}

	key, err := r.client.BuildKey("PK", settingsPK(customerID), "SK", "SETTINGS")
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
		return fmt.Errorf("failed to restore customer: %w", err)
	}
	return nil
}
