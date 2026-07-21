package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

type passkeyListResponse struct {
	Credentials []models.PasskeyCredential `json:"credentials"`
}

// Route handler that returns all passkeys for an account
func (s *Service) GetPasskeysHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

// Route handler that adds a passkey to an account
func (s *Service) AddPasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.PasskeyCredential
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	if input.CredentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.AddPasskey(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "passkey"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

// Route handler that updates a passkey for an account
func (s *Service) UpdatePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "passkey"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Route handler that deletes a passkey for an account
func (s *Service) DeletePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
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

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "passkey"))
	wtr.WriteHeader(http.StatusNoContent)
}
