package api

import (
	"errors"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Helper that sends appropriate error response based on error type
func sendAccountError(wtr http.ResponseWriter, req *http.Request, err error) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		accountID = req.PathValue("id")
	}

	switch {
	case errors.Is(err, ErrNotFound):
		logger.Warn("resource not found", err, logger.Attr("account_id", accountID))
		httpErr.SendError(wtr, req, httpErr.Global.NotFound)
		return
	case errors.Is(err, ErrPasskeySignCountRegression):
		httpErr.SendError(wtr, req, models.Err.PasskeySignCountRegression)
		return
	case errors.Is(err, ErrPasskeyAlreadyExists):
		httpErr.SendError(wtr, req, models.Err.PasskeyAlreadyExists)
		return
	case errors.Is(err, ErrAlreadyExists):
		httpErr.SendError(wtr, req, models.Err.AlreadyExists)
		return
	case errors.Is(err, ErrForbiddenNamespace):
		httpErr.SendError(wtr, req, models.Err.ForbiddenNamespace)
		return
	case errors.Is(err, ErrInvalidUnsubscribeToken):
		httpErr.SendError(wtr, req, models.Err.InvalidUnsubscribeToken)
		return
	case errors.Is(err, ErrInvalidUnsubscribeChannel):
		httpErr.SendError(wtr, req, models.Err.InvalidUnsubscribeChannel)
		return
	case errors.Is(err, ErrInvalidCommunicationChannel):
		httpErr.SendError(wtr, req, models.Err.InvalidCommunicationChannel)
		return
	case errors.Is(err, ErrInvalidInput):
		httpErr.SendError(wtr, req, models.Err.InvalidInput)
		return
	case errors.Is(err, ErrVersionConflict):
		httpErr.SendError(wtr, req, models.Err.VersionConflict)
		return
	}

	logger.Error("internal error", err, logger.Attr("account_id", accountID))
	httpErr.SendError(wtr, req, httpErr.Global.Internal)
}
