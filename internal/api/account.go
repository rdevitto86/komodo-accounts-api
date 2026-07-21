package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/segmentio/ksuid"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"

	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

var (
	ErrNotFound                    = errors.New("not found")
	ErrAlreadyExists               = errors.New("already exists")
	ErrPasskeyAlreadyExists        = errors.New("passkey already exists")
	ErrPasskeySignCountRegression  = errors.New("passkey sign count regression")
	ErrForbiddenNamespace          = errors.New("forbidden namespace")
	ErrInvalidUnsubscribeToken     = errors.New("invalid unsubscribe token")
	ErrInvalidUnsubscribeChannel   = errors.New("invalid unsubscribe channel")
	ErrInvalidInput                = errors.New("invalid input")
	ErrAccountNotPendingDeletion   = errors.New("account not pending deletion")
	ErrInvalidCommunicationChannel = errors.New("invalid communication channel")
	ErrVersionConflict             = errors.New("version conflict")
)

func (s *Service) GetProfile(ctx context.Context, accountID string) (*models.Account, error) {
	if account, ok := s.profileCache.Get(accountID); ok {
		return account, nil
	}

	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account profile: %w", err)
	}
	s.profileCache.Set(accountID, account, 60*time.Second)
	return account, nil
}

func (s *Service) CreateAccount(ctx context.Context, account *models.Account) error {
	if account.AccountID == "" || account.Email == "" || account.FirstName == "" || account.LastName == "" {
		return fmt.Errorf("invalid account: %w", errors.New("account_id, email, first_name, and last_name are required"))
	}

	now := time.Now().UTC()
	account.CreatedAt = now
	account.UpdatedAt = now
	if account.AuthMethods == nil {
		account.AuthMethods = []string{}
	}
	if err := s.repo.CreateAccount(ctx, account); err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("failed to create account: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create account: %w", err)
	}
	return nil
}

func (s *Service) UpdateProfile(ctx context.Context, accountID string, update *models.Account) (*models.Account, error) {
	req := &models.UpdateProfileRequest{}
	if update.Phone != "" {
		req.Phone = &update.Phone
	}
	if update.FirstName != "" {
		req.FirstName = &update.FirstName
	}
	if update.LastName != "" {
		req.LastName = &update.LastName
	}
	if update.AvatarURL != "" {
		req.AvatarURL = &update.AvatarURL
	}

	updated, err := s.repo.UpdateAccount(ctx, accountID, req)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update account profile: %w", err)
	}
	s.profileCache.Delete(accountID)
	return updated, nil
}

func (s *Service) DeleteProfile(ctx context.Context, accountID string) error {
	var email string
	profile, profileErr := s.GetProfile(ctx, accountID)
	if profileErr != nil {
		if !errors.Is(profileErr, ErrNotFound) {
			return fmt.Errorf("failed to get account profile: %w", profileErr)
		}
	} else {
		email = strings.ToLower(profile.Email)
	}

	if err := s.repo.DeleteAccount(ctx, accountID); err != nil {
		return fmt.Errorf("failed to delete account profile: %w", err)
	}
	s.profileCache.Delete(accountID)
	if email != "" {
		s.credentialsCache.Delete(email)
	}
	if err := s.deleteS3Exports(ctx, accountID); err != nil {
		return err
	}
	return nil
}

func (s *Service) SoftDeleteProfile(ctx context.Context, accountID string) error {
	if err := s.repo.SoftDeleteAccount(ctx, accountID); err != nil {
		return fmt.Errorf("failed to soft-delete profile: %w", err)
	}
	s.profileCache.Delete(accountID)
	if profile, err := s.repo.GetAccount(ctx, accountID); err == nil {
		s.credentialsCache.Delete(strings.ToLower(profile.Email))
	}
	return nil
}

func (s *Service) RestoreProfile(ctx context.Context, accountID string) error {
	if err := s.repo.RestoreAccount(ctx, accountID); err != nil {
		if errors.Is(err, db.ErrAccountNotPendingDeletion) {
			return fmt.Errorf("account not eligible for restore: %w", ErrAccountNotPendingDeletion)
		}
		return fmt.Errorf("failed to restore profile: %w", err)
	}
	return nil
}

func (s *Service) GetAddresses(ctx context.Context, accountID string) ([]models.Address, error) {
	addrs, err := s.repo.GetAccountAddresses(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account addresses: %w", err)
	}
	return addrs, nil
}

func (s *Service) AddAddress(ctx context.Context, accountID string, addr *models.Address) error {
	if err := s.repo.CreateAddress(ctx, accountID, addr); err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("address already exists: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to add address: %w", err)
	}
	if addr.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, accountID, addr.AddressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

func (s *Service) UpdateAddress(ctx context.Context, accountID, addressID string, req *models.UpdateAddressRequest) error {
	if err := s.repo.UpdateAddress(ctx, accountID, addressID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update address: %w", err)
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, accountID, addressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

func (s *Service) DeleteAddress(ctx context.Context, accountID, addressID string) error {
	if err := s.repo.DeleteAddress(ctx, accountID, addressID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}

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
		if err := s.repo.SetPaymentDefault(ctx, accountID, pm.PaymentID); err != nil {
			return fmt.Errorf("failed to enforce default payment method: %w", err)
		}
	}
	return nil
}

func (s *Service) DeletePayment(ctx context.Context, accountID, paymentID string) error {
	if err := s.repo.DeletePayment(ctx, accountID, paymentID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("payment method not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete payment method: %w", err)
	}
	return nil
}

func (s *Service) GetPreferences(ctx context.Context, accountID string) (*models.Preferences, error) {
	prefs, err := s.repo.GetAccountPreferences(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account preferences: %w", err)
	}
	return prefs, nil
}

func (s *Service) UpdatePreferences(ctx context.Context, accountID string, req *models.UpdatePreferencesRequest) error {
	if req.Communication != nil {
		for k := range req.Communication {
			if !models.ValidCommunicationChannels[k] {
				return fmt.Errorf("invalid communication channel %q: %w", k, ErrInvalidCommunicationChannel)
			}
		}
	}
	if err := s.repo.UpdateAccountPreferences(ctx, accountID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update account preferences: %w", err)
	}
	return nil
}

func (s *Service) DeletePreferences(ctx context.Context, accountID string) error {
	if err := s.repo.DeleteAccountPreferences(ctx, accountID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete account preferences: %w", err)
	}
	return nil
}

func (s *Service) GetCredentials(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	cacheKey := strings.ToLower(email)
	if creds, ok := s.credentialsCache.Get(cacheKey); ok {
		return creds, nil
	}
	creds, err := s.repo.GetAccountCredentialsByEmail(ctx, email)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account credentials: %w", err)
	}
	s.credentialsCache.Set(cacheKey, creds, 60*time.Second)
	return creds, nil
}

func (s *Service) UpdateCredentials(ctx context.Context, accountID string, req *models.UpdateCredentialsRequest) error {
	if accountID == "" {
		return fmt.Errorf("invalid credentials update: %w", errors.New("account_id is required"))
	}
	if req.PasswordHash == "" && len(req.AuthMethods) == 0 {
		return fmt.Errorf("invalid credentials update: %w", errors.New("at least one of password_hash or auth_methods is required"))
	}
	if err := s.repo.UpdateAccountCredentials(ctx, accountID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update credentials: %w", err)
	}
	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		logger.Warn("failed to resolve account email for cache invalidation", err, logger.Attr("account_id", accountID))
		return nil
	}
	s.credentialsCache.Delete(strings.ToLower(account.Email))
	return nil
}

func (s *Service) CheckAccountExists(ctx context.Context, email string) (*models.AccountExistsResponse, error) {
	result, err := s.repo.GetAccountExistsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check if account exists: %w", err)
	}
	return result, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, db.ErrNotFound)
}

func (s *Service) GetPasskeys(ctx context.Context, accountID string) ([]models.PasskeyCredential, error) {
	creds, err := s.repo.GetAccountPasskeys(ctx, accountID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get account passkeys: %w", err)
	}
	return creds, nil
}

func (s *Service) AddPasskey(ctx context.Context, accountID string, cred *models.PasskeyCredential) error {
	if err := s.repo.CreatePasskey(ctx, accountID, cred); err != nil {
		if errors.Is(err, db.ErrPasskeyAlreadyExists) {
			return fmt.Errorf("passkey already exists: %w", ErrPasskeyAlreadyExists)
		}
		return fmt.Errorf("failed to add passkey: %w", err)
	}
	return nil
}

func (s *Service) UpdatePasskey(ctx context.Context, accountID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error) {
	cred, err := s.repo.UpdatePasskey(ctx, accountID, credentialID, update)
	if err != nil {
		if errors.Is(err, db.ErrPasskeySignCountRegression) {
			return nil, fmt.Errorf("passkey sign count regression: %w", ErrPasskeySignCountRegression)
		}
		if isNotFound(err) {
			return nil, fmt.Errorf("passkey not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update passkey: %w", err)
	}
	return cred, nil
}

func (s *Service) DeletePasskey(ctx context.Context, accountID, credentialID string) error {
	if err := s.repo.DeletePasskey(ctx, accountID, credentialID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("passkey not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}

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

func (s *Service) UpdateSettings(ctx context.Context, accountID string, req *models.UpdateSettingsRequest) (*models.AccountSettings, error) {
	if req.Status != nil {
		if err := validateStatus(*req.Status); err != nil {
			return nil, err
		}
	}
	if err := s.repo.UpdateSettingsPartial(ctx, accountID, req, req.Version); err != nil {
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

func validateStatus(s string) error {
	switch s {
	case "active", "suspended", "closed", "pending_deletion":
		return nil
	}
	return fmt.Errorf("invalid account status %q: %w", s, ErrInvalidInput)
}

var tagNamespaceMap = map[string]string{
	"loyalty-api":            "loyalty.",
	"marketing-api":          "marketing.",
	"promotions-api":         "marketing.",
	"customer-servicing-api": "support.",
	"accounts-api":           "system.",
}

var tagRE = regexp.MustCompile(`^[a-z0-9._]{1,32}$`)

func (s *Service) UpdateSettingsTags(ctx context.Context, accountID, callerService string, req *models.UpdateSettingsTagsRequest) (*models.AccountSettings, error) {
	prefix, ok := tagNamespaceMap[callerService]
	if !ok {
		return nil, fmt.Errorf("unknown service %q: %w", callerService, ErrForbiddenNamespace)
	}
	for _, tag := range append(req.Add, req.Remove...) {
		if !tagRE.MatchString(tag) {
			return nil, fmt.Errorf("invalid tag %q: must match [a-z0-9._]{1,32}: %w", tag, ErrForbiddenNamespace)
		}
		if !strings.HasPrefix(tag, prefix) {
			return nil, fmt.Errorf("tag %q not in namespace %q: %w", tag, prefix, ErrForbiddenNamespace)
		}
	}

	current, err := s.repo.GetSettings(ctx, accountID)
	if err != nil {
		if !isNotFound(err) {
			return nil, fmt.Errorf("failed to load settings for tag mutation: %w", err)
		}
		defaults := &models.AccountSettings{Status: "active"}
		if createErr := s.repo.UpdateSettings(ctx, accountID, defaults); createErr != nil {
			return nil, fmt.Errorf("failed to initialize settings for tag mutation: %w", createErr)
		}
		current = defaults
	}

	if expectedTagCount(current.Tags, req.Add, req.Remove) > 20 {
		return nil, fmt.Errorf("tag limit exceeded (max 20): %w", ErrForbiddenNamespace)
	}

	if err := s.repo.MutateSettingsTags(ctx, accountID, req, req.Version); err != nil {
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

type profileExportData struct {
	settings       *models.AccountSettings
	prefs          *models.Preferences
	addrs          []models.Address
	payments       []models.PaymentMethod
	consentHistory []models.ConsentLog
	passkeys       []models.PasskeyCredential
}

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

func (s *Service) ExportProfile(ctx context.Context, accountID string) (*models.ExportProfileResponse, error) {
	if s.s3 == nil {
		return nil, fmt.Errorf("export not configured")
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
		passkeyExports[i] = models.PasskeyExport{
			CredentialID:   pk.CredentialID,
			SignCount:      pk.SignCount,
			Transports:     pk.Transports,
			AAGUID:         pk.AAGUID,
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

	downloadURL, err := s.s3.PresignGet(ctx, s.exportBucket, key, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to generate export download url: %w", err)
	}

	return &models.ExportProfileResponse{
		ExportID:    exportID,
		DownloadURL: downloadURL,
		ExpiresAt:   time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339),
	}, nil
}

func (s *Service) deleteS3Exports(ctx context.Context, accountID string) error {
	if s.s3 == nil {
		return nil
	}
	prefix := "exports/" + accountID + "/"
	keys, err := s.s3.ListObjects(ctx, s.exportBucket, prefix)
	if err != nil {
		logger.Error("failed to list s3 exports for erasure", err, logger.Attr("account_id", accountID))
		return fmt.Errorf("failed to list s3 exports for erasure: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.s3.DeleteObjects(ctx, s.exportBucket, keys); err != nil {
		logger.Error("failed to delete s3 exports for erasure", err, logger.Attr("account_id", accountID))
		return fmt.Errorf("failed to delete s3 exports for erasure: %w", err)
	}
	return nil
}

type unsubPayload struct {
	AccountID string `json:"account_id"`
	Channel   string `json:"channel"`
	Exp       int64  `json:"exp"`
	JTI       string `json:"jti"`
}

func (s *Service) MintUnsubscribeToken(ctx context.Context, accountID, channel string) (string, error) {
	if len(s.unsubscribeKey) == 0 {
		return "", fmt.Errorf("unsubscribe key not configured")
	}
	if !models.ValidCommunicationChannels[channel] {
		return "", fmt.Errorf("invalid unsubscribe channel %q: %w", channel, ErrInvalidUnsubscribeChannel)
	}
	if _, err := s.repo.GetAccount(ctx, accountID); err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return "", fmt.Errorf("failed to verify account for token mint: %w", err)
	}
	payload, err := json.Marshal(unsubPayload{
		AccountID: accountID,
		Channel:   channel,
		Exp:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		JTI:       ksuid.New().String(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal unsubscribe payload: %w", err)
	}
	mac := hmac.New(sha256.New, s.unsubscribeKey)
	mac.Write(payload)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(append(payload, sig...))
	return token, nil
}

func parseUnsubToken(payloadBytes []byte) (*unsubPayload, error) {
	var p unsubPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, fmt.Errorf("failed to parse unsubscribe payload: %w", ErrInvalidUnsubscribeToken)
	}
	if p.JTI == "" {
		return nil, fmt.Errorf("missing jti in unsubscribe token: %w", ErrInvalidUnsubscribeToken)
	}
	if len(p.JTI) > 256 {
		p.JTI = p.JTI[:256]
	}
	if time.Now().Unix() > p.Exp {
		return nil, fmt.Errorf("unsubscribe token expired: %w", ErrInvalidUnsubscribeToken)
	}
	if !models.ValidCommunicationChannels[p.Channel] {
		return nil, fmt.Errorf("invalid unsubscribe channel %q: %w", p.Channel, ErrInvalidUnsubscribeChannel)
	}
	return &p, nil
}

func (s *Service) GetAvatarUploadURL(ctx context.Context, accountID string) (string, error) {
	if s.s3 == nil {
		return "", fmt.Errorf("avatar upload not configured")
	}
	key := "avatars/" + accountID + "/avatar"
	url, err := s.s3.PresignPut(ctx, s.avatarBucket, key, 15*time.Minute, "", 0)
	if err != nil {
		return "", fmt.Errorf("failed to generate avatar upload url: %w", err)
	}
	return url, nil
}

func (s *Service) VerifyAndRecordUnsubscribe(ctx context.Context, token, ipAddr, userAgent string) error {
	if len(s.unsubscribeKey) == 0 {
		return fmt.Errorf("unsubscribe key not configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) < 32 {
		return fmt.Errorf("invalid unsubscribe token: %w", ErrInvalidUnsubscribeToken)
	}
	payloadBytes := raw[:len(raw)-32]
	sig := raw[len(raw)-32:]

	mac := hmac.New(sha256.New, s.unsubscribeKey)
	mac.Write(payloadBytes)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return fmt.Errorf("invalid unsubscribe token signature: %w", ErrInvalidUnsubscribeToken)
	}

	p, err := parseUnsubToken(payloadBytes)
	if err != nil {
		return err
	}
	if len(ipAddr) > 256 {
		ipAddr = ipAddr[:256]
	}
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}

	existing, err := s.repo.GetLatestConsent(ctx, p.AccountID, p.Channel)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("failed to check consent log for replay: %w", err)
	}
	if existing != nil && existing.SourceRef == p.JTI {
		return nil
	}

	entry := &models.ConsentLog{
		Channel:   p.Channel,
		Action:    "opt_out",
		Source:    "unsubscribe_link",
		SourceRef: p.JTI,
		IPAddress: ipAddr,
		UserAgent: userAgent,
	}
	if err := s.repo.AppendConsentLog(ctx, p.AccountID, entry); err != nil {
		return fmt.Errorf("failed to record unsubscribe: %w", err)
	}
	return nil
}
