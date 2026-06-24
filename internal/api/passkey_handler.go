package api

import (
	"encoding/json"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"

	"komodo-user-api/internal/models"
)

type passkeyListResponse struct {
	Credentials []models.PasskeyCredential `json:"credentials"`
}

func (s *Service) GetPasskeysHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	creds, err := s.GetPasskeys(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, passkeyListResponse{Credentials: creds})
}

func (s *Service) AddPasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.PasskeyCredential
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	if input.CredentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.AddPasskey(req.Context(), userID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

func (s *Service) UpdatePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	credentialID := req.PathValue("credential_id")
	if credentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	var input models.PasskeyCredential
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdatePasskey(req.Context(), userID, credentialID, &input)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

func (s *Service) DeletePasskeyHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	credentialID := req.PathValue("credential_id")
	if credentialID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeletePasskey(req.Context(), userID, credentialID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}
