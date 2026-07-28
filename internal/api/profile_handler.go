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
		logger.Warn("rejected unauthorized request", nil)
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
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.CreateAccountRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	created, err := s.CreateAccount(req.Context(), accountID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("created account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, created)
}

// Route handler that updates the profile for the authenticated user.
func (s *Service) UpdateProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.UpdateAccountRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdateProfile(req.Context(), accountID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Route handler that deletes the profile for the authenticated user.
func (s *Service) DeleteProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.SoftDeleteProfile(req.Context(), accountID); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("requested account closure", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusAccepted)
	writeJSON(wtr, map[string]string{"message": "account closure requested; data will be erased in 30 days"})
}

// Route handler that restores the profile for the authenticated user.
func (s *Service) RestoreProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
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

	logger.Info("restored account resource", nil, logger.Attr("account_id", accountID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, map[string]string{"message": "account restored"})
}

// Route handler that provides an upload URL for the account's avatar image
func (s *Service) AvatarUploadHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.AvatarUploadRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	uploadURL, err := s.GetAvatarUploadURL(req.Context(), accountID, string(input.ContentType), input.ContentLength)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, models.AvatarUploadResponse{
		UploadUrl:        uploadURL,
		ExpiresInSeconds: int(s.presignTTL.Seconds()),
		MaxBytes:         MaxAvatarBytes,
	})
}

// Helper that extracts the account ID from the JWT token in the request context.
func accountIDFromJWT(req *http.Request) string {
	id, _ := req.Context().Value(ctxKeys.USER_ID_KEY).(string)
	return id
}
