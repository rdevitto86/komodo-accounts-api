package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	httpReq "github.com/rdevitto86/komodo-forge-sdk-go/api/request"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that verifies an unsubscribe token and records the unsubscribe event
func (s *Service) UnsubscribeHandler(wtr http.ResponseWriter, req *http.Request) {
	var input models.UnsubscribeRequest
	if err := decodeStrict(req, &input); err != nil || input.Token == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	ip := httpReq.GetClientKey(req)
	ua := req.Header.Get("Account-Agent")

	if err := s.VerifyAndRecordUnsubscribe(req.Context(), input.Token, ip, ua); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}

// Route handler that mints an unsubscribe token for a given account and channel
func (s *Service) MintUnsubscribeTokenHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.MintUnsubscribeTokenRequest
	if err := decodeStrict(req, &input); err != nil || input.Channel == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	token, err := s.MintUnsubscribeToken(req.Context(), accountID, input.Channel)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, models.MintUnsubscribeTokenResponse{Token: token})
}
