package api

import (
	"encoding/json"
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
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}
	input.CustomerID = userID

	if err := s.CreateUser(req.Context(), &input); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
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
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
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

	if err := s.DeleteProfile(req.Context(), userID); err != nil {
		sendUserError(wtr, req, err)
		return
	}

	logger.Info("user resource updated", nil, logger.Attr("customer_id", userID), logger.Attr("resource", "profile"))
	wtr.WriteHeader(http.StatusNoContent)
}
