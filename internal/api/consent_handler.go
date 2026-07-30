package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func (s *Service) GetConsentsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

func (s *Service) RecordConsentHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}
