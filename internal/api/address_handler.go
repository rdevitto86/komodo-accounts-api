package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that returns all addresses for an account
func (s *Service) GetAddressesHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

// Route handler that adds a new address for an account
func (s *Service) AddAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

// Route handler that updates an existing address for an account
func (s *Service) UpdateAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusOK)
}

// Route handler that deletes an existing address for an account
func (s *Service) DeleteAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusNoContent)
}
