package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

var (
	tagRE           = regexp.MustCompile(`^[a-z0-9._]{1,32}$`)
	tagNamespaceMap = map[string]string{
		"loyalty-api":            "loyalty.",
		"marketing-api":          "marketing.",
		"promotions-api":         "marketing.",
		"customer-servicing-api": "support.",
		"accounts-api":           "system.",
	}
)

// Route handler that retrieves account settings
func (s *Service) GetSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		accountID = accountIDFromJWT(req)
	}
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	settings, err := s.GetSettings(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, settings)
}

// Route handler that fetches the authenticated user's settings
func (s *Service) GetMySettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	httpErr.SendError(wtr, req, httpErr.Global.NotImplemented)
}

// Route handler that updates settings for an account
func (s *Service) UpdateSettingsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.UpdateSettingsRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	if input.Status != nil {
		callerSvc := callerServiceFromScopes(req)
		if callerSvc != "customer-servicing-api" && callerSvc != "accounts-api" {
			httpErr.SendError(wtr, req, models.Err.ForbiddenNamespace)
			return
		}
	}

	updated, err := s.UpdateSettings(req.Context(), accountID, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "settings"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Route handler that updates settings tags on an account
func (s *Service) UpdateSettingsTagsHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	callerSvc := callerServiceFromScopes(req)
	if callerSvc == "" {
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.UpdateSettingsTagsRequest
	if err := decodeStrict(req, &input); err != nil {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	updated, err := s.UpdateSettingsTags(req.Context(), accountID, callerSvc, &input)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("updated account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "settings.tags"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusOK)
	writeJSON(wtr, updated)
}

// Fetches settings for an account from the DB.
func (s *Service) GetSettings(ctx context.Context, accountID string) (*models.AccountSettings, error) {
	settings, err := s.repo.GetSettings(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("settings not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	return settings, nil
}

// Updates settings for an account in the DB.
func (s *Service) UpdateSettings(ctx context.Context, accountID string, req *models.UpdateSettingsRequest) (*models.AccountSettings, error) {
	if req.Status != nil {
		if err := validateStatus(string(*req.Status)); err != nil {
			return nil, err
		}
	}

	version := 0
	if req.Version != nil {
		version = *req.Version
	}

	if err := s.repo.UpdateSettingsPartial(ctx, accountID, req, version); err != nil {
		if errors.Is(err, db.ErrVersionConflict) {
			return nil, fmt.Errorf("failed to update settings due to version conflict: %w", ErrVersionConflict)
		}
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update settings: %w", err)
	}

	settings, err := s.repo.GetSettings(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read settings after update: %w", err)
	}
	return settings, nil
}

// Updates settings tags for an account in the DB.
func (s *Service) UpdateSettingsTags(
	ctx context.Context, accountID, callerService string, req *models.UpdateSettingsTagsRequest,
) (*models.AccountSettings, error) {
	prefix, ok := tagNamespaceMap[callerService]
	if !ok {
		return nil, fmt.Errorf("unknown service %q: %w", callerService, ErrForbiddenNamespace)
	}

	add := parseSlice(req.Add)
	remove := parseSlice(req.Remove)
	if err := validateTagNamespace(prefix, add, remove); err != nil {
		return nil, err
	}

	current, err := s.repo.GetSettings(ctx, accountID)
	if err != nil {
		if !isNotFound(err) {
			return nil, fmt.Errorf("failed to load settings for tag mutation: %w", err)
		}
		defaults := &models.AccountSettings{Status: models.AccountSettingsStatus("active")}
		if createErr := s.repo.UpdateSettings(ctx, accountID, defaults); createErr != nil {
			return nil, fmt.Errorf("failed to initialize settings for tag mutation: %w", createErr)
		}
		current = defaults
	}

	if expectedTagCount(parseSlice(current.Tags), add, remove) > 20 {
		return nil, fmt.Errorf("tag limit exceeded (max 20): %w", ErrForbiddenNamespace)
	}

	tagVersion := 0
	if req.Version != nil {
		tagVersion = *req.Version
	}
	if err := s.repo.MutateSettingsTags(ctx, accountID, req, tagVersion); err != nil {
		if errors.Is(err, db.ErrVersionConflict) {
			return nil, fmt.Errorf("failed to update settings tags due to version conflict: %w", ErrVersionConflict)
		}
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update tags: %w", err)
	}
	settings, err := s.repo.GetSettings(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read settings after tag mutation: %w", err)
	}
	return settings, nil
}

// Helper function to extract service name from request scopes
func callerServiceFromScopes(req *http.Request) string {
	scopes, _ := req.Context().Value(ctxKeys.SCOPES_KEY).([]string)
	for _, s := range scopes {
		if strings.HasPrefix(s, "svc:") {
			return strings.TrimPrefix(s, "svc:")
		}
	}
	return ""
}

// Validates tag namespace and format.
func validateTagNamespace(prefix string, add, remove []string) error {
	for _, tag := range append(append([]string{}, add...), remove...) {
		if !tagRE.MatchString(tag) {
			return fmt.Errorf("invalid tag %q: must match [a-z0-9._]{1,32}: %w", tag, ErrForbiddenNamespace)
		}
		if !strings.HasPrefix(tag, prefix) {
			return fmt.Errorf("tag %q not in namespace %q: %w", tag, prefix, ErrForbiddenNamespace)
		}
	}
	return nil
}

// Calculates the expected tag count after a tag mutation.
func expectedTagCount(current, add, remove []string) int {
	removeSet := make(map[string]bool, len(remove))
	for _, t := range remove {
		removeSet[t] = true
	}
	existing := make(map[string]bool, len(current))
	for _, t := range current {
		existing[t] = true
	}
	count := 0
	for t := range existing {
		if !removeSet[t] {
			count++
		}
	}
	for _, t := range add {
		if !existing[t] {
			count++
		}
	}
	return count
}

// Validates an account status.
func validateStatus(s string) error {
	switch strings.ToLower(s) {
	case "active", "suspended", "closed":
		return nil
	}
	return fmt.Errorf("invalid account status %q: %w", s, ErrInvalidInput)
}

// Parse out a specific subset of list items.
func parseSlice[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}
