package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"komodo-accounts-api/internal/models"
	"net/http"
	"time"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/segmentio/ksuid"
)

type profileExportData struct {
	settings       *models.AccountSettings
	prefs          *models.Preferences
	addrs          []models.Address
	payments       []models.PaymentMethod
	consentHistory []models.ConsentLog
	passkeys       []models.PasskeyCredential
}

// Route handler that exports the user's profile data.
func (s *Service) ExportProfileHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := accountIDFromJWT(req)
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	result, err := s.ExportProfile(req.Context(), accountID)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	logger.Info("exported account resource", nil,
		logger.Attr("account_id", accountID),
		logger.Attr("resource", "profile.export"),
	)

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, result)
}

// Export profile data for an account from the DB.
func (s *Service) ExportProfile(ctx context.Context, accountID string) (*models.ExportProfileResponse, error) {
	if s.s3 == nil {
		return nil, errors.New("export not configured")
	}

	profile, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account profile for export: %w", err)
	}

	d, err := s.gatherProfileExportData(ctx, accountID)
	if err != nil {
		return nil, err
	}

	passkeyExports := make([]models.PasskeyExport, len(d.passkeys))
	for i, pk := range d.passkeys {
		transports := []string{}
		if pk.Transports != nil {
			transports = *pk.Transports
		}
		aaguid := ""
		if pk.Aaguid != nil {
			aaguid = *pk.Aaguid
		}
		passkeyExports[i] = models.PasskeyExport{
			CredentialID:   pk.CredentialId,
			SignCount:      pk.SignCount,
			Transports:     transports,
			AAGUID:         aaguid,
			BackupEligible: pk.BackupEligible,
			BackupState:    pk.BackupState,
			CreatedAt:      pk.CreatedAt,
			LastUsedAt:     pk.LastUsedAt,
		}
	}

	export := &models.ProfileExport{
		Profile:        profile,
		Settings:       d.settings,
		Preferences:    d.prefs,
		Addresses:      d.addrs,
		Payments:       d.payments,
		ConsentHistory: d.consentHistory,
		Passkeys:       passkeyExports,
	}

	data, err := json.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal export: %w", err)
	}

	exportID := ksuid.New().String()
	key := "exports/" + accountID + "/" + exportID + ".json"

	if err := s.s3.PutObject(ctx, s.exportBucket, key, data, "application/json"); err != nil {
		return nil, fmt.Errorf("failed to write export to s3: %w", err)
	}

	downloadURL, err := s.s3.PresignGet(ctx, s.exportBucket, key, s.presignTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate export download url: %w", err)
	}

	expiresAt := time.Now().UTC().Add(s.presignTTL)
	return &models.ExportProfileResponse{
		ExportId:    &exportID,
		DownloadUrl: &downloadURL,
		ExpiresAt:   &expiresAt,
	}, nil
}

// Gathers all profile data for export from the DB.
func (s *Service) gatherProfileExportData(ctx context.Context, accountID string) (*profileExportData, error) {
	d := &profileExportData{}
	var err error

	d.settings, err = s.repo.GetSettings(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get settings for export: %w", err)
	}

	d.prefs, err = s.repo.GetAccountPreferences(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get preferences for export: %w", err)
	}

	d.addrs, err = s.repo.GetAccountAddresses(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get addresses for export: %w", err)
	}

	d.payments, err = s.repo.ListPayments(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get payments for export: %w", err)
	}

	d.consentHistory, err = s.repo.ListConsentHistory(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get consent history for export: %w", err)
	}

	d.passkeys, err = s.repo.GetAccountPasskeys(ctx, accountID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get passkeys for export: %w", err)
	}

	return d, nil
}
