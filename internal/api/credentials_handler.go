package api

import (
	"encoding/json"
	"net/http"
	"strings"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"

	"komodo-user-api/internal/models"
)

func (s *Service) GetCredentialsHandler(wtr http.ResponseWriter, req *http.Request) {
	email := strings.TrimSpace(req.URL.Query().Get("email"))
	if email == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("email query parameter is required"))
		return
	}

	creds, err := s.GetCredentials(req.Context(), email)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, creds)
}

func (s *Service) UpdateCredentialsHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := req.PathValue("id")
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("user id is required"))
		return
	}

	var input models.UpdateCredentialsRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdateCredentials(req.Context(), userID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}

func (s *Service) GetUserExistsHandler(wtr http.ResponseWriter, req *http.Request) {
	email := strings.TrimSpace(req.URL.Query().Get("email"))
	if email == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest, httpErr.WithDetail("email query parameter is required"))
		return
	}

	result, err := s.CheckUserExists(req.Context(), email)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, result)
}
