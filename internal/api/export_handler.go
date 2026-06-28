package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func (s *Service) ExportProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	result, err := s.ExportProfile(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile.export"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, result)
}
