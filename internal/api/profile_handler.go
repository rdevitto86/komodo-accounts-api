package api

import (
	"errors"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-customer-api/internal/models"
)

func userIDFromJWT(req *http.Request) string {
	id, _ := req.Context().Value(ctxKeys.USER_ID_KEY).(string)
	return id
}

func userIDFromPath(req *http.Request) string {
	return req.PathValue("id")
}

func (s *Service) GetProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromPath(req)
	if userID == "" {
		userID = userIDFromJWT(req)
	}
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	user, err := s.GetProfile(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, user)
}

func (s *Service) CreateUserHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.User
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	input.CustomerID = userID

	if err := s.CreateUser(req.Context(), &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource created", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, input)
}

func (s *Service) UpdateProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.User
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdateProfile(req.Context(), userID, &input)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

func (s *Service) DeleteProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.SoftDeleteProfile(req.Context(), userID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusAccepted)
	writeJSON(wtr, map[string]string{"message": "account closure requested; data will be erased in 30 days"})
}

func (s *Service) RestoreProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	if err := s.RestoreProfile(req.Context(), userID); err != nil {
		if errors.Is(err, ErrAccountNotPendingDeletion) {
			httpErr.SendError(wtr, req, models.Err.AccountNotPendingDeletion)
			return
		}
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, map[string]string{"message": "account restored"})
}

func (s *Service) AvatarUploadHandler(wtr http.ResponseWriter, req *http.Request) {
	userID := userIDFromJWT(req)
	if userID == "" {
		logger.Warn("unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	uploadURL, err := s.GetAvatarUploadURL(req.Context(), userID)
	if err != nil {
		sendUserError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, map[string]any{
		"upload_url":         uploadURL,
		"expires_in_seconds": 900,
	})
}
