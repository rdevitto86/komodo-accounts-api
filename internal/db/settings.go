package db

import (
	"context"
	"errors"
	"fmt"
	"komodo-accounts-api/internal/models"
	"strings"
	"time"

	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const settingsSK = "SETTINGS"

type settingsRecord struct {
	PK              string     `dynamodbav:"PK"`
	SK              string     `dynamodbav:"SK"`
	EmailVerified   bool       `dynamodbav:"email_verified"`
	EmailVerifiedAt *time.Time `dynamodbav:"email_verified_at,omitempty"`
	PhoneVerified   bool       `dynamodbav:"phone_verified"`
	PhoneVerifiedAt *time.Time `dynamodbav:"phone_verified_at,omitempty"`
	Status          string     `dynamodbav:"status"`
	StatusReason    string     `dynamodbav:"status_reason,omitempty"`
	StatusChangedAt *time.Time `dynamodbav:"status_changed_at,omitempty"`
	Tags            []string   `dynamodbav:"tags,omitempty,stringset"`
	Version         int        `dynamodbav:"version"`
}

// DB method that retrieves the account settings.
func (r *Repo) GetSettings(ctx context.Context, accountID string) (*models.AccountSettings, error) {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", settingsSK)
	if err != nil {
		return nil, fmt.Errorf("failed to build settings key: %w", err)
	}

	var record settingsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	s := record.toModel()
	return &s, nil
}

// DB method that updates the account settings.
func (r *Repo) UpdateSettingsPartial(ctx context.Context, accountID string, req *models.UpdateSettingsRequest, version int) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", settingsSK)
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
		exprValues[":st"] = &ddbTypes.AttributeValueMemberS{Value: string(*req.Status)}
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
	record := settingsRecordFrom(createPK(accountID), settingsSK, s)
	if err := r.client.WriteItemFrom(ctx, r.table, record, false, nil, nil); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

// DB method that modifies the tags on an account's settings.
func (r *Repo) MutateSettingsTags(ctx context.Context, accountID string, req *models.UpdateSettingsTagsRequest, version int) error {
	add := derefSlice(req.Add)
	remove := derefSlice(req.Remove)
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}

	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", settingsSK)
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

	if len(add) > 0 {
		addVals := make([]string, len(add))
		copy(addVals, add)
		clauses = append(clauses, "ADD #tags :addSet")
		exprValues[":addSet"] = &ddbTypes.AttributeValueMemberSS{Value: addVals}
	}
	if len(remove) > 0 {
		removeVals := make([]string, len(remove))
		copy(removeVals, remove)
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

// Helper that converts a settingsRecord model to a AccountSettings model.
func (r *settingsRecord) toModel() models.AccountSettings {
	return models.AccountSettings{
		EmailVerified:   &r.EmailVerified,
		EmailVerifiedAt: r.EmailVerifiedAt,
		PhoneVerified:   &r.PhoneVerified,
		PhoneVerifiedAt: r.PhoneVerifiedAt,
		Status:          models.AccountSettingsStatus(r.Status),
		StatusReason:    optStr(r.StatusReason),
		StatusChangedAt: r.StatusChangedAt,
		Tags:            ptrSlice(r.Tags),
		Version:         r.Version,
	}
}

// Helper that converts a AccountSettings model to a settingsRecord model.
func settingsRecordFrom(pk, sk string, s *models.AccountSettings) settingsRecord {
	return settingsRecord{
		PK:              pk,
		SK:              sk,
		EmailVerified:   deref(s.EmailVerified),
		EmailVerifiedAt: s.EmailVerifiedAt,
		PhoneVerified:   deref(s.PhoneVerified),
		PhoneVerifiedAt: s.PhoneVerifiedAt,
		Status:          string(s.Status),
		StatusReason:    deref(s.StatusReason),
		StatusChangedAt: s.StatusChangedAt,
		Tags:            derefSlice(s.Tags),
		Version:         s.Version,
	}
}
