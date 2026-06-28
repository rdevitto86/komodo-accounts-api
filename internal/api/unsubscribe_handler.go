package api

import (
	"encoding/json"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	httpReq "github.com/rdevitto86/komodo-forge-sdk-go/api/request"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func (s *Service) UnsubscribeHandler(wtr http.ResponseWriter, req *http.Request) {
	var input models.UnsubscribeRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil || input.Token == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	ip := httpReq.GetClientKey(req)
	ua := req.Header.Get("User-Agent")

	if err := s.VerifyAndRecordUnsubscribe(req.Context(), input.Token, ip, ua); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}

func (s *Service) MintUnsubscribeTokenHandler(wtr http.ResponseWriter, req *http.Request) {
	customerID := userIDFromPath(req)
	if customerID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.MintUnsubscribeTokenRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil || input.Channel == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	token, err := s.MintUnsubscribeToken(req.Context(), customerID, input.Channel)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, models.MintUnsubscribeTokenResponse{Token: token})
}
