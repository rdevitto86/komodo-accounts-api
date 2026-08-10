package api

import (
	"context"
	"fmt"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that fetches the preferences for an account.
func (s *Service) GetPreferencesHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	prefs, err := s.GetPreferences(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, prefs)
}

// Route handler that updates the preferences for an account.
func (s *Service) UpdatePreferencesHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Preferences
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdatePreferences(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "preferences"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, input)
}

// Fetches account preferences for an account from the DB.
func (s *Service) GetPreferences(ctx context.Context, accountID string) (*models.Preferences, error) {
	prefs, err := s.repo.GetAccountPreferences(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account preferences: %w", err)
	}
	return prefs, nil
}

// Updates account preferences in the DB.
func (s *Service) UpdatePreferences(ctx context.Context, accountID string, req *models.Preferences) error {
	if req.Communication != nil {
		for k := range *req.Communication {
			if !models.ValidCommunicationChannels[k] {
				return fmt.Errorf("invalid communication channel %q: %w", k, ErrInvalidCommunicationChannel)
			}
		}
	}
	if err := s.repo.UpdateAccountPreferences(ctx, accountID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update account preferences: %w", err)
	}
	return nil
}
