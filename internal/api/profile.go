package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/models"
)

const maxAvatarBytes = 2 * 1024 * 1024 // 2

var validAvatarContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

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

	logger.Info("created account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "profile"),
	)

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

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "profile"),
	)

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

	var input models.DeleteProfileRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if err := s.DeleteProfile(req.Context(), accountID, string(input.ConfirmEmail)); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("erased account", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "profile"),
	)

	wtr.WriteHeader(http.StatusNoContent)
}

// Route handler that updates the account's email address.
func (s *Service) UpdateEmailHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

// Route handler that provides an upload URL for the account's avatar image.
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

	uploadURL, err := s.GetAvatarUploadURL(
		req.Context(),
		accountID,
		string(input.ContentType),
		input.ContentLength,
	)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, models.AvatarUploadResponse{
		UploadUrl:        uploadURL,
		ExpiresInSeconds: int(s.presignTTL.Seconds()),
		MaxBytes:         maxAvatarBytes,
	})
}

// Route handker that deletes the account's avatar image.
func (s *Service) DeleteAvatarHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

// Fetches a S3 upload URL for an account profile avatar.
func (s *Service) GetAvatarUploadURL(
	ctx context.Context, accountID, contentType string, contentLength int64,
) (string, error) {
	if s.s3 == nil {
		return "", errors.New("avatar upload not configured")
	}
	if !validAvatarContentTypes[contentType] {
		return "", fmt.Errorf("invalid avatar content type %q: %w", contentType, ErrInvalidInput)
	}
	if contentLength <= 0 || contentLength > maxAvatarBytes {
		return "", fmt.Errorf(
			"avatar size %d bytes outside allowed range (1..%d): %w",
			contentLength, maxAvatarBytes, ErrInvalidInput,
		)
	}

	url, err := s.s3.PresignPut(ctx, s.avatarBucket, avatarKey(accountID), s.presignTTL, contentType, contentLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate avatar upload url: %w", err)
	}
	return url, nil
}

// Creates a unique profile avatar path.
func avatarKey(accountID string) string {
	return "avatars/" + accountID + "/avatar"
}

// Extracts the account ID from the JWT token in the request context.
func accountIDFromJWT(req *http.Request) string {
	return ctxKeys.GetSubject(req.Context())
}
