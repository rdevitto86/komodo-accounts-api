package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"komodo-accounts-api/internal/clients"
	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
	eventsmodels "komodo-accounts-api/internal/models/events"

	"github.com/rdevitto86/komodo-forge-sdk-go/events/domain"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

// Retrieves an account profile (with caching).
func (s *Service) GetProfile(ctx context.Context, accountID string) (*models.Account, error) {
	if account, ok := s.profileCache.Get(accountID); ok {
		return account, nil
	}

	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account profile: %w", err)
	}
	s.profileCache.Set(accountID, account, s.cacheTTL)
	return account, nil
}

// Creates a new account profile.
func (s *Service) CreateAccount(ctx context.Context, accountID string, req *models.CreateAccountRequest) (*models.Account, error) {
	if accountID == "" || req.Email == "" || req.FirstName == "" || req.LastName == "" {
		return nil, fmt.Errorf("account_id, email, first_name, and last_name are required: %w", ErrInvalidInput)
	}

	created, err := s.repo.CreateAccount(ctx, accountID, req)
	if err != nil {
		if errors.Is(err, db.ErrEmailAlreadyExists) {
			return nil, fmt.Errorf("failed to create account: %w", ErrEmailAlreadyExists)
		}
		if errors.Is(err, db.ErrAlreadyExists) {
			return nil, fmt.Errorf("failed to create account: %w", ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to create account: %w", err)
	}

	event, buildErr := clients.NewAccountRegisteredEvent(created.AccountId, eventsmodels.AccountRegisteredPayload{
		AccountID:   created.AccountId,
		Email:       string(created.Email),
		Username:    created.Username,
		FirstName:   created.FirstName,
		LastName:    created.LastName,
		AuthMethods: []string{},
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	})
	s.publishAccountEvent(ctx, created.AccountId, clients.EventTypeAccountRegistered, event, buildErr)

	return created, nil
}

// Updates an account profile.
func (s *Service) UpdateProfile(ctx context.Context, accountID string, req *models.UpdateAccountRequest) (*models.Account, error) {
	var oldPhone string
	phoneDiffKnown := false
	if req.Phone != nil {
		if before, err := s.GetProfile(ctx, accountID); err == nil {
			phoneDiffKnown = true
			if before.Phone != nil {
				oldPhone = *before.Phone
			}
		} else {
			logger.Warn("failed to fetch prior profile for phone-change diff", logger.Attr("account_id", accountID))
		}
	}

	updated, err := s.repo.UpdateAccount(ctx, accountID, req)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update account profile: %w", err)
	}
	s.profileCache.Delete(accountID)

	if changedFields := updateProfileChangedFields(req); len(changedFields) > 0 {
		event, buildErr := clients.NewAccountProfileUpdatedEvent(accountID, eventsmodels.AccountProfileUpdatedPayload{
			AccountID:     accountID,
			ChangedFields: changedFields,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		})
		s.publishAccountEvent(ctx, accountID, clients.EventTypeAccountProfileUpdated, event, buildErr)
	}

	if req.Phone != nil && phoneDiffKnown && oldPhone != *req.Phone {
		event, buildErr := clients.NewAccountPhoneChangedEvent(accountID, eventsmodels.AccountPhoneChangedPayload{
			AccountID: accountID,
			OldPhone:  oldPhone,
			NewPhone:  *req.Phone,
		})
		s.publishAccountEvent(ctx, accountID, clients.EventTypeAccountPhoneChanged, event, buildErr)
	}

	return updated, nil
}

func updateProfileChangedFields(req *models.UpdateAccountRequest) []string {
	var fields []string
	if req.AvatarUrl != nil {
		fields = append(fields, "avatar_url")
	}
	if req.FirstName != nil {
		fields = append(fields, "first_name")
	}
	if req.LastName != nil {
		fields = append(fields, "last_name")
	}
	if req.Phone != nil {
		fields = append(fields, "phone")
	}
	return fields
}

// Deletes an account profile.
func (s *Service) DeleteProfile(ctx context.Context, accountID, confirmEmail string) error {
	confirmation := strings.TrimSpace(confirmEmail)
	if confirmation == "" {
		return fmt.Errorf("confirm_email is required to close an account: %w", ErrInvalidInput)
	}

	profile, err := s.GetProfile(ctx, accountID)
	if err != nil {
		return err
	}

	email := strings.ToLower(strings.TrimSpace(string(profile.Email)))
	if !strings.EqualFold(confirmation, email) {
		return fmt.Errorf("confirm_email does not match the account email: %w", ErrInvalidInput)
	}

	if err := s.deleteS3Exports(ctx, accountID); err != nil {
		return err
	}
	if err := s.deleteAvatarObject(ctx, accountID); err != nil {
		return err
	}

	if err := s.repo.DeleteAccount(ctx, accountID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete account profile: %w", err)
	}

	s.profileCache.Delete(accountID)
	if email != "" {
		s.credentialsCache.Delete(email)
	}

	event, buildErr := clients.NewAccountDeletedEvent(accountID, eventsmodels.AccountDeletedPayload{AccountID: accountID})
	s.publishAccountEvent(ctx, accountID, clients.EventTypeAccountDeleted, event, buildErr)

	return nil
}

// Checks if an account exists in the DB.
func (s *Service) CheckAccountExists(ctx context.Context, email string) (*models.AccountExistsResponse, error) {
	result, err := s.repo.GetAccountExistsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check if account exists: %w", err)
	}
	return result, nil
}

// Deletes all S3 objects under the export bucket for a given account.
func (s *Service) deleteS3Exports(ctx context.Context, accountID string) error {
	if s.s3 == nil {
		return nil
	}

	prefix := "exports/" + accountID + "/"
	keys, err := s.s3.ListObjects(ctx, s.exportBucket, prefix)
	if err != nil {
		logger.Error("failed to list s3 exports for erasure", err, logger.Attr("account_id", accountID))
		return fmt.Errorf("failed to list s3 exports for erasure: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.s3.DeleteObjects(ctx, s.exportBucket, keys); err != nil {
		logger.Error("failed to delete s3 exports for erasure", err, logger.Attr("account_id", accountID))
		return fmt.Errorf("failed to delete s3 exports for erasure: %w", err)
	}
	return nil
}

// Deletes an account profile avatar object from S3.
func (s *Service) deleteAvatarObject(ctx context.Context, accountID string) error {
	if s.s3 == nil {
		return nil
	}
	if err := s.s3.DeleteObject(ctx, s.avatarBucket, avatarKey(accountID)); err != nil {
		logger.Error("failed to delete s3 avatar", err, logger.Attr("account_id", accountID))
		return fmt.Errorf("failed to delete s3 avatar: %w", err)
	}
	return nil
}

const publishEventTimeout = 2 * time.Second

func (s *Service) publishAccountEvent(ctx context.Context, accountID, eventType string, event domain.Event, buildErr error) {
	if buildErr != nil {
		logger.Error("failed to build domain event", buildErr, logger.Attr("account_id", accountID), logger.Attr("event_type", eventType))
		return
	}
	if s.eventPublisher == nil {
		return
	}
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishEventTimeout)
	defer cancel()
	if err := s.eventPublisher.Publish(publishCtx, event); err != nil {
		logger.Error("failed to publish domain event", err, logger.Attr("account_id", accountID), logger.Attr("event_type", eventType))
	}
}
