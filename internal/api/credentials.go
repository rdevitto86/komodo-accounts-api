package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that returns credentials for an account (by email)
func (s *Service) GetCredentialsHandler(wtr http.ResponseWriter, req *http.Request) {
	email := strings.TrimSpace(req.URL.Query().Get("email"))
	if email == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("email query parameter is required"))
		return
	}

	creds, err := s.GetCredentials(req.Context(), email)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, creds)
}

// Route handler that updates credentials for an account
func (s *Service) UpdateCredentialsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("account id is required"))
		return
	}

	var input models.UpdateCredentialsRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdateCredentials(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "credentials"))
	wtr.WriteHeader(http.StatusNoContent)
}

// Route handler that checks if an account exists (by email)
func (s *Service) GetAccountExistsHandler(wtr http.ResponseWriter, req *http.Request) {
	email := strings.TrimSpace(req.URL.Query().Get("email"))
	if email == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("email query parameter is required"))
		return
	}

	result, err := s.CheckAccountExists(req.Context(), email)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, result)
}

// Fetches account credentials for an account from the DB.
func (s *Service) GetCredentials(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	cacheKey := strings.ToLower(email)
	if creds, ok := s.credentialsCache.Get(cacheKey); ok {
		return creds, nil
	}
	creds, err := s.repo.GetAccountCredentialsByEmail(ctx, email)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account credentials: %w", err)
	}
	s.credentialsCache.Set(cacheKey, creds, s.cacheTTL)
	return creds, nil
}

// Updates account credentials for an account in the DB.
func (s *Service) UpdateCredentials(ctx context.Context, accountID string, req *models.UpdateCredentialsRequest) error {
	if accountID == "" {
		return fmt.Errorf("account_id is required: %w", ErrInvalidInput)
	}
	if req.PasswordHash == nil && req.AuthMethods == nil {
		return fmt.Errorf("at least one of password_hash or auth_methods is required: %w", ErrInvalidInput)
	}
	if err := s.repo.UpdateAccountCredentials(ctx, accountID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update credentials: %w", err)
	}
	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		logger.Warn("failed to resolve account email for cache invalidation", err, logger.Attr("account_id", accountID))
		return nil
	}
	s.credentialsCache.Delete(strings.ToLower(string(account.Email)))
	return nil
}
