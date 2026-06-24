package api

import (
	"encoding/json"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"

	"komodo-user-api/internal/models"
)

// GetPreferencesHandler returns preferences for the authenticated user.
func (s *Service) GetPreferencesHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	prefs, err := s.GetPreferences(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, prefs)
}

// UpdatePreferencesHandler updates preferences for the authenticated user.
func (s *Service) UpdatePreferencesHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Preferences
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpdatePreferences(req.Context(), userID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, input)
}

// DeletePreferencesHandler removes preferences for the authenticated user.
func (s *Service) DeletePreferencesHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := resolveUserID(req)
	if userID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.DeletePreferences(req.Context(), userID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}
