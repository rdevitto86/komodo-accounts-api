package clients

import (
	"fmt"
	"strings"

	"github.com/rdevitto86/komodo-forge-sdk-go/events/domain"
	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http/client"

	eventsmodels "komodo-accounts-api/internal/models/events"
)

const (
	eventSource       = "komodo-accounts-api"
	eventsPublishPath = "/publish"
	eventVersionV1    = "1"
	entityTypeAccount = "account"

	EventTypeAccountRegistered         = "account.registered"
	EventTypeAccountDeleted            = "account.deleted"
	EventTypeAccountProfileUpdated     = "account.profile_updated"
	EventTypeAccountEmailChanged       = "account.email_changed"
	EventTypeAccountPhoneChanged       = "account.phone_changed"
	EventTypeAccountConsentChanged     = "account.consent_changed"
	EventTypeAccountPreferencesUpdated = "account.preferences_updated"
	EventTypeAccountStatusChanged      = "account.status_changed"
	EventTypeAccountTagsChanged        = "account.tags_changed"
	EventTypeAccountPasskeyAdded       = "account.passkey_added"
	EventTypeAccountPasskeyRemoved     = "account.passkey_removed"
)

func NewPublisher(cfg Config, authedClient *sdkhttp.Client) (*domain.Publisher, error) {
	if cfg.EventsAPIBaseURL == "" {
		return nil, fmt.Errorf("events api base url is required")
	}
	if authedClient == nil {
		return nil, fmt.Errorf("authenticated http client is required")
	}

	sink, err := domain.NewHTTPSink(authedClient, strings.TrimRight(cfg.EventsAPIBaseURL, "/")+eventsPublishPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create events http sink: %w", err)
	}

	publisher, err := domain.NewPublisher(sink)
	if err != nil {
		return nil, fmt.Errorf("failed to create domain event publisher: %w", err)
	}
	return publisher, nil
}

func NewAccountRegisteredEvent(accountID string, payload eventsmodels.AccountRegisteredPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountRegistered, accountID, payload)
}

func NewAccountDeletedEvent(accountID string, payload eventsmodels.AccountDeletedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountDeleted, accountID, payload)
}

func NewAccountProfileUpdatedEvent(accountID string, payload eventsmodels.AccountProfileUpdatedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountProfileUpdated, accountID, payload)
}

func NewAccountEmailChangedEvent(accountID string, payload eventsmodels.AccountEmailChangedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountEmailChanged, accountID, payload)
}

func NewAccountPhoneChangedEvent(accountID string, payload eventsmodels.AccountPhoneChangedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountPhoneChanged, accountID, payload)
}

func NewAccountConsentChangedEvent(accountID string, payload eventsmodels.AccountConsentChangedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountConsentChanged, accountID, payload)
}

func NewAccountPreferencesUpdatedEvent(accountID string, payload eventsmodels.AccountPreferencesUpdatedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountPreferencesUpdated, accountID, payload)
}

func NewAccountStatusChangedEvent(accountID string, payload eventsmodels.AccountStatusChangedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountStatusChanged, accountID, payload)
}

func NewAccountTagsChangedEvent(accountID string, payload eventsmodels.AccountTagsChangedPayload) (domain.Event, error) {
	if payload.Added == nil {
		payload.Added = []string{}
	}
	if payload.Removed == nil {
		payload.Removed = []string{}
	}
	return newAccountEvent(EventTypeAccountTagsChanged, accountID, payload)
}

func NewAccountPasskeyAddedEvent(accountID string, payload eventsmodels.AccountPasskeyAddedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountPasskeyAdded, accountID, payload)
}

func NewAccountPasskeyRemovedEvent(accountID string, payload eventsmodels.AccountPasskeyRemovedPayload) (domain.Event, error) {
	return newAccountEvent(EventTypeAccountPasskeyRemoved, accountID, payload)
}

func newAccountEvent(eventType, accountID string, payload any) (domain.Event, error) {
	event, err := domain.New(domain.NewInput{
		Type:       eventType,
		Source:     eventSource,
		EntityID:   accountID,
		EntityType: entityTypeAccount,
		Version:    eventVersionV1,
		Payload:    payload,
	})
	if err != nil {
		return domain.Event{}, fmt.Errorf("failed to build %s event: %w", eventType, err)
	}
	return event, nil
}
