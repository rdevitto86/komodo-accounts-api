package api

import (
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "credentials"))
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
