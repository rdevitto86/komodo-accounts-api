package api

import (
	"net/http"
	"strings"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func (s *Service) GetSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	customerID := userIDFromPath(req)
	if customerID == "" {
		customerID = userIDFromJWT(req)
	}
	if customerID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	settings, err := s.GetSettings(req.Context(), customerID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, settings)
}

func (s *Service) UpdateSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	customerID := userIDFromPath(req)
	if customerID == "" {
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
		if callerSvc != "customer-servicing-api" && callerSvc != "customer-api" {
			httpErr.SendError(wtr, req, models.Err.ForbiddenNamespace)
			return
		}
	}

	updated, err := s.UpdateSettings(req.Context(), customerID, &input)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", customerID), logger.Attr("resource", "settings"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

func callerServiceFromScopes(req *http.Request) string {
	scopes, _ := req.Context().Value(ctxKeys.SCOPES_KEY).([]string)
	for _, s := range scopes {
		if strings.HasPrefix(s, "svc:") {
			return strings.TrimPrefix(s, "svc:")
		}
	}
	return ""
}

func (s *Service) UpdateSettingsTagsHandler(wtr http.ResponseWriter, req *http.Request) {
	customerID := userIDFromPath(req)
	if customerID == "" {
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

	updated, err := s.UpdateSettingsTags(req.Context(), customerID, callerSvc, &input)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", customerID), logger.Attr("resource", "settings.tags"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}
