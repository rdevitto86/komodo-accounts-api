package api

import (
	"net/http"
	"strings"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that retrieves account settings
func (s *Service) GetSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	settings, err := s.GetSettings(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, settings)
}

// Route handler that updates settings for an account
func (s *Service) UpdateSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.UpdateSettingsRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if input.Status != nil {
		callerSvc := callerServiceFromScopes(req)
		if callerSvc != "customer-servicing-api" && callerSvc != "accounts-api" {
			httpErr.SendError(wtr, req, models.Err.ForbiddenNamespace)
			return
		}
	}

	updated, err := s.UpdateSettings(req.Context(), accountID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "settings"))

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Helper function to extract service name from request scopes
func callerServiceFromScopes(req *http.Request) string {
	scopes, _ := req.Context().Value(ctxKeys.SCOPES_KEY).([]string)
	for _, s := range scopes {
		if strings.HasPrefix(s, "svc:") {
			return strings.TrimPrefix(s, "svc:")
		}
	}
	return ""
}

// Route handler that updates settings tags on an account
func (s *Service) UpdateSettingsTagsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	callerSvc := callerServiceFromScopes(req)
	if callerSvc == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.UpdateSettingsTagsRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdateSettingsTags(req.Context(), accountID, callerSvc, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "settings.tags"))

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}
