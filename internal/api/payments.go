package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

// Route handler that returns all payment methods for an account.
func (s *Service) GetPaymentsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	payments, err := s.GetPayments(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, payments)
}

// Route handler that fetches a payment method for an account.
func (s *Service) GetPaymentHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if req.PathValue("id") == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

// Route handler that upserts a payment method for an account.
func (s *Service) UpsertPaymentHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.PaymentMethod
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.UpsertPayment(req.Context(), accountID, &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "payment"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, input)
}

// Route handler that deletes a payment method for an account.
func (s *Service) DeletePaymentHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	paymentID := req.PathValue("id")
	if paymentID == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeletePayment(req.Context(), accountID, paymentID); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("deleted account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "payment"),
	)
	wtr.WriteHeader(http.StatusNoContent)
}

// Fetches all payment methods for an account from the DB.
func (s *Service) GetPayments(ctx context.Context, accountID string) ([]models.PaymentMethod, error) {
	methods, err := s.repo.ListPayments(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get payment methods: %w", err)
	}
	return methods, nil
}

// Upserts a payment method for an account to the DB.
func (s *Service) UpsertPayment(ctx context.Context, accountID string, pm *models.PaymentMethod) error {
	if err := s.repo.UpsertPayment(ctx, accountID, pm); err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("payment method already exists: %w", ErrAlreadyExists)
		}
		if isNotFound(err) {
			return fmt.Errorf("payment method not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to upsert payment method: %w", err)
	}
	if pm.IsDefault {
		if err := s.repo.SetPaymentDefault(ctx, accountID, pm.PaymentId); err != nil {
			return fmt.Errorf("failed to enforce default payment method: %w", err)
		}
	}
	return nil
}

// Deletes a payment method for an account from the DB.
func (s *Service) DeletePayment(ctx context.Context, accountID, paymentID string) error {
	if err := s.repo.DeletePayment(ctx, accountID, paymentID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("payment method not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete payment method: %w", err)
	}
	return nil
}
