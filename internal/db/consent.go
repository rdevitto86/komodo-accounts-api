package db

import (
	"context"
	"fmt"
	"komodo-accounts-api/internal/models"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
)

const consentSKTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

type consentRecord struct {
	PK         string    `dynamodbav:"PK"`
	SK         string    `dynamodbav:"SK"`
	Channel    string    `dynamodbav:"channel"`
	Action     string    `dynamodbav:"action"`
	Source     string    `dynamodbav:"source"`
	SourceRef  string    `dynamodbav:"source_ref,omitempty"`
	IPAddress  string    `dynamodbav:"ip_address,omitempty"`
	UserAgent  string    `dynamodbav:"user_agent,omitempty"`
	RecordedAt time.Time `dynamodbav:"recorded_at"`
}

// DB method that retrieves the latest consent record for a given channel.
func (r *Repo) GetLatestConsent(ctx context.Context, accountID, channel string) (*models.ConsentLog, error) {
	var records []consentRecord
	var query = dynamodb.QueryInput{
		TableName:              r.table,
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :skPrefix)",
		ExpressionValues: map[string]ddbTypes.AttributeValue{
			":pk":       &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
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
	log := records[0].toModel()
	return &log, nil
}

// DB client method that appends a consent log entry for an account.
func (r *Repo) AppendConsentLog(ctx context.Context, accountID string, entry *models.ConsentLog) error {
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	record := consentRecordFrom(createPK(accountID), consentSK(string(entry.Channel), entry.RecordedAt), entry)
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
			":pk":     &ddbTypes.AttributeValueMemberS{Value: createPK(accountID)},
			":prefix": &ddbTypes.AttributeValueMemberS{Value: "CONSENT#"},
		},
		Limit: aws.Int32(1000),
	}

	if _, err := r.client.QueryAs(ctx, query, &records); err != nil {
		return nil, fmt.Errorf("failed to list consent history: %w", err)
	}

	logs := make([]models.ConsentLog, len(records))
	for i := range records {
		logs[i] = records[i].toModel()
	}
	return logs, nil
}

// Helper that converts a consentRecord model to a ConsentLog model.
func (r *consentRecord) toModel() models.ConsentLog {
	return models.ConsentLog{
		Channel:    models.ConsentLogChannel(r.Channel),
		Action:     models.ConsentLogAction(r.Action),
		Source:     r.Source,
		SourceRef:  optStr(r.SourceRef),
		IpAddress:  optStr(r.IPAddress),
		UserAgent:  optStr(r.UserAgent),
		RecordedAt: r.RecordedAt,
	}
}

// Helper that converts a ConsentLog model to a consentRecord model.
func consentRecordFrom(pk, sk string, c *models.ConsentLog) consentRecord {
	return consentRecord{
		PK:         pk,
		SK:         sk,
		Channel:    string(c.Channel),
		Action:     string(c.Action),
		Source:     c.Source,
		SourceRef:  deref(c.SourceRef),
		IPAddress:  deref(c.IpAddress),
		UserAgent:  deref(c.UserAgent),
		RecordedAt: c.RecordedAt,
	}
}

func consentSK(channel string, at time.Time) string {
	return "CONSENT#" + channel + "#" + at.UTC().Format(consentSKTimeFormat)
}
