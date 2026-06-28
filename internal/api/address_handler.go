package api

import (
	"encoding/json"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func (s *Service) GetAddressesHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromPath(req)
	if userID == "" {
		userID = userIDFromJWT(req)
	}
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addrs, err := s.GetAddresses(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, addrs)
}

func (s *Service) AddAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Address
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.AddAddress(req.Context(), userID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "address"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

func (s *Service) UpdateAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addressID := req.PathValue("id")
	if addressID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	var input models.Address
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdateAddress(req.Context(), userID, addressID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusOK)
}

func (s *Service) DeleteAddressHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	addressID := req.PathValue("id")
	if addressID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeleteAddress(req.Context(), userID, addressID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "address"))
	wtr.WriteHeader(http.StatusNoContent)
}
