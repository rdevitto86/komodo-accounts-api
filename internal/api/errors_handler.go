package api

import (
	"errors"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func sendUserError(wtr http.ResponseWriter, req *http.Request, err error) {
	userID := userIDFromJWT(req)
	if errors.Is(err, ErrNotFound) {
		logger.Warn("resource not found", err, logger.Attr("customer_id", userID))
		httpErr.SendError(wtr, req, httpErr.Global.NotFound)
		return
	}
	if errors.Is(err, ErrPasskeyAlreadyExists) {
		httpErr.SendError(wtr, req, models.Err.PasskeyAlreadyExists)
		return
	}
	if errors.Is(err, ErrAlreadyExists) {
		httpErr.SendError(wtr, req, models.Err.AlreadyExists)
		return
	}
	if errors.Is(err, ErrForbiddenNamespace) {
		httpErr.SendError(wtr, req, models.Err.ForbiddenNamespace)
		return
	}
	if errors.Is(err, ErrMarketingConsentMismatch) {
		httpErr.SendError(wtr, req, models.Err.MarketingConsentMismatch)
		return
	}
	logger.Error("internal error", err, logger.Attr("customer_id", userID))
	httpErr.SendError(wtr, req, httpErr.Global.Internal)
}
