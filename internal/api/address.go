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

// Route handler that returns all addresses for an account
func (s *Service) GetAddressesHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addrs, err := s.GetAddresses(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, addrs)
}

// Route handler that returns a specific address for an account.
func (s *Service) GetAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if req.PathValue("id") == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

// Route handler that adds a new address for an account
func (s *Service) AddAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Address
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.AddAddress(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("created account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

// Route handler that updates an existing address for an account
func (s *Service) UpdateAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addressID := req.PathValue("id")
	if addressID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	var input models.UpdateAddressRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdateAddress(req.Context(), accountID, addressID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusOK)
}

// Route handler that deletes an existing address for an account
func (s *Service) DeleteAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addressID := req.PathValue("id")
	if addressID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeleteAddress(req.Context(), accountID, addressID); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("deleted account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusNoContent)
}

// Fetches all account addresses from the DB.
func (s *Service) GetAddresses(ctx context.Context, accountID string) ([]models.Address, error) {
	addrs, err := s.repo.GetAccountAddresses(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account addresses: %w", err)
	}
	return addrs, nil
}

// Adds an address to an account to the DB.
func (s *Service) AddAddress(ctx context.Context, accountID string, addr *models.Address) error {
	if err := s.repo.CreateAddress(ctx, accountID, addr); err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("address already exists: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to add address: %w", err)
	}
	if addr.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, accountID, addr.AddressId); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

// Updates an address for an account in the DB.
func (s *Service) UpdateAddress(ctx context.Context, accountID, addressID string, req *models.UpdateAddressRequest) error {
	if err := s.repo.UpdateAddress(ctx, accountID, addressID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update address: %w", err)
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, accountID, addressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

// Deletes an address for an account from the DB.
func (s *Service) DeleteAddress(ctx context.Context, accountID, addressID string) error {
	if err := s.repo.DeleteAddress(ctx, accountID, addressID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}
