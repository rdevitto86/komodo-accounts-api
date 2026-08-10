package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

type passkeyListResponse struct {
	Credentials []models.PasskeyCredential `json:"credentials"`
}

// Route handler that returns all passkeys for an account.
func (s *Service) GetPasskeysHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	creds, err := s.GetPasskeys(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, passkeyListResponse{Credentials: creds})
}

// Route handler that adds a passkey to an account.
func (s *Service) AddPasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.PasskeyCredential
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	if input.CredentialId == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.AddPasskey(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("created account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "passkey"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

// Route handler that updates a passkey for an account.
func (s *Service) UpdatePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	credentialID := req.PathValue("credential_id")
	if credentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	var input models.PasskeyCredential
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdatePasskey(req.Context(), accountID, credentialID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "passkey"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Route handler that deletes a passkey for an account.
func (s *Service) DeletePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	credentialID := req.PathValue("credential_id")
	if credentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeletePasskey(req.Context(), accountID, credentialID); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("deleted account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "passkey"),
	)
	wtr.WriteHeader(http.StatusNoContent)
}

// Fetches all passkeys for an account from the DB.
func (s *Service) GetPasskeys(ctx context.Context, accountID string) ([]models.PasskeyCredential, error) {
	creds, err := s.repo.GetAccountPasskeys(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account passkeys: %w", err)
	}
	return creds, nil
}

// Adds a passkey to an account from the DB.
func (s *Service) AddPasskey(ctx context.Context, accountID string, cred *models.PasskeyCredential) error {
	if err := s.repo.CreatePasskey(ctx, accountID, cred); err != nil {
		if errors.Is(err, db.ErrPasskeyAlreadyExists) {
			return fmt.Errorf("passkey already exists: %w", ErrPasskeyAlreadyExists)
		}
		return fmt.Errorf("failed to add passkey: %w", err)
	}
	return nil
}

// Updates a passkey for an account from the DB.
func (s *Service) UpdatePasskey(
	ctx context.Context, accountID, credentialID string, update *models.PasskeyCredential,
) (*models.PasskeyCredential, error) {
	cred, err := s.repo.UpdatePasskey(ctx, accountID, credentialID, update)
	if err != nil {
		if errors.Is(err, db.ErrPasskeySignCountRegression) {
			return nil, fmt.Errorf("passkey sign count regression: %w", ErrPasskeySignCountRegression)
		}
		if isNotFound(err) {
			return nil, fmt.Errorf("passkey not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update passkey: %w", err)
	}
	return cred, nil
}

// Deletes a passkey for an account from the DB.
func (s *Service) DeletePasskey(ctx context.Context, accountID, credentialID string) error {
	if err := s.repo.DeletePasskey(ctx, accountID, credentialID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("passkey not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}
