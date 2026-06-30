package api

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func (s *Service) GetPaymentsHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromPath(req)
	if userID == "" {
		userID = userIDFromJWT(req)
	}
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	payments, err := s.GetPayments(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, payments)
}

func (s *Service) UpsertPaymentHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.PaymentMethod
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpsertPayment(req.Context(), userID, &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "payment"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, input)
}

func (s *Service) DeletePaymentHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	paymentID := req.PathValue("id")
	if paymentID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeletePayment(req.Context(), userID, paymentID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "payment"))
	wtr.WriteHeader(http.StatusNoContent)
}
