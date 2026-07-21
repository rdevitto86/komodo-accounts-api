package api

import (
	"errors"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

// Route handler that retrieves the profile for the authenticated user or a specific account.
func (s *Service) GetProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	account, err := s.GetProfile(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, account)
}

// Route handler that creates a new account for the authenticated user.
func (s *Service) CreateAccountHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Account
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	input.AccountID = accountID

	if err := s.CreateAccount(req.Context(), &input); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource created", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

// Route handler that updates the profile for the authenticated user.
func (s *Service) UpdateProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.Account
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdateProfile(req.Context(), accountID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Route handler that deletes the profile for the authenticated user.
func (s *Service) DeleteProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.SoftDeleteProfile(req.Context(), accountID); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusAccepted)
	writeJSON(wtr, map[string]string{"message": "account closure requested; data will be erased in 30 days"})
}

// Route handler that restores the profile for the authenticated user.
func (s *Service) RestoreProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.RestoreProfile(req.Context(), accountID); err != nil {
		if errors.Is(err, ErrAccountNotPendingDeletion) {
			httpErr.SendError(wtr, req, models.Err.AccountNotPendingDeletion)
			return
		}
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("account resource updated", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, map[string]string{"message": "account restored"})
}

// Route handler that provides an upload URL for the account's avatar image
func (s *Service) AvatarUploadHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	uploadURL, err := s.GetAvatarUploadURL(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, map[string]any{
		"upload_url":         uploadURL,
		"expires_in_seconds": 900,
	})
}

// Helper that extracts the account ID from the JWT token in the request context.
func accountIDFromJWT(req *http.Request) string {
	id, _ := req.Context().Value(ctxKeys.USER_ID_KEY).(string)
	return id
}
