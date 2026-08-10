package db

import (
	"context"
	"fmt"
	"komodo-accounts-api/internal/models"
	"strings"

	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const prefsSK = "PREFS"

type prefsRecord struct {
	PK            string          `dynamodbav:"PK"`
	SK            string          `dynamodbav:"SK"`
	Language      string          `dynamodbav:"language"`
	Timezone      string          `dynamodbav:"timezone"`
	Communication map[string]bool `dynamodbav:"communication"`
}

// DB client method that retrieves account preferences.
func (r *Repo) GetAccountPreferences(ctx context.Context, accountID string) (*models.Preferences, error) {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", prefsSK)
	if err != nil {
		return nil, fmt.Errorf("failed to build preferences key: %w", err)
	}

	var record prefsRecord
	if err := r.client.GetItemAs(ctx, r.table, key, false, nil, &record); err != nil {
		return nil, fmt.Errorf("failed to get preferences: %w", err)
	}
	prefs := record.toModel()
	return &prefs, nil
}

// DB client method that updates account preferences.
func (r *Repo) UpdateAccountPreferences(ctx context.Context, accountID string, req *models.Preferences) error {
	key, err := r.client.BuildKey("PK", createPK(accountID), "SK", prefsSK)
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
		m := make(map[string]ddbTypes.AttributeValue, len(*req.Communication))
		for k, v := range *req.Communication {
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

// Helper that converts a preferences record to a Preferences model.
func (r *prefsRecord) toModel() models.Preferences {
	return models.Preferences{
		Language:      optStr(r.Language),
		Timezone:      optStr(r.Timezone),
		Communication: optMap(r.Communication),
	}
}
