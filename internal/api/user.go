package api

import (
	"bytes"
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

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/segmentio/ksuid"

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"

	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

var ErrNotFound = errors.New("not found")
var ErrAlreadyExists = errors.New("already exists")
var ErrPasskeyAlreadyExists = errors.New("passkey already exists")
var ErrPasskeySignCountRegression = errors.New("passkey sign count regression")
var ErrForbiddenNamespace = errors.New("forbidden namespace")
var ErrInvalidUnsubscribeToken = errors.New("invalid unsubscribe token")
var ErrInvalidUnsubscribeChannel = errors.New("invalid unsubscribe channel")
var ErrInvalidInput = errors.New("invalid input")
var ErrAccountNotPendingDeletion = errors.New("account not pending deletion")
var ErrInvalidCommunicationChannel = errors.New("invalid communication channel")
var ErrVersionConflict = errors.New("version conflict")

func (s *Service) GetProfile(ctx context.Context, userID string) (*models.User, error) {
	if user, ok := s.profileCache.Get(userID); ok {
		return user, nil
	}
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	s.profileCache.Set(userID, user, 60*time.Second)
	return user, nil
}

func (s *Service) CreateUser(ctx context.Context, user *models.User) error {
	if user.CustomerID == "" || user.Email == "" || user.FirstName == "" || user.LastName == "" {
		return fmt.Errorf("invalid user: %w", errors.New("customer_id, email, first_name, and last_name are required"))
	}
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.AuthMethods == nil {
		user.AuthMethods = []string{}
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("failed to create user: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func (s *Service) UpdateProfile(ctx context.Context, userID string, update *models.User) (*models.User, error) {
	update.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.UpdateUser(ctx, userID, update)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update user profile: %w", err)
	}
	s.profileCache.Delete(userID)
	return updated, nil
}

func (s *Service) DeleteProfile(ctx context.Context, userID string) error {
	var email string
	profile, profileErr := s.GetProfile(ctx, userID)
	if profileErr != nil {
		if !errors.Is(profileErr, ErrNotFound) {
			return fmt.Errorf("failed to get user profile: %w", profileErr)
		}
	} else {
		email = strings.ToLower(profile.Email)
	}

	if err := s.repo.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete user profile: %w", err)
	}
	s.profileCache.Delete(userID)
	if email != "" {
		s.credentialsCache.Delete(email)
	}
	if err := s.deleteS3Exports(ctx, userID); err != nil {
		return err
	}
	return nil
}

func (s *Service) SoftDeleteProfile(ctx context.Context, userID string) error {
	if err := s.repo.SoftDeleteCustomer(ctx, userID); err != nil {
		return fmt.Errorf("failed to soft-delete profile: %w", err)
	}
	s.profileCache.Delete(userID)
	if profile, err := s.repo.GetUser(ctx, userID); err == nil {
		s.credentialsCache.Delete(strings.ToLower(profile.Email))
	}
	return nil
}

func (s *Service) RestoreProfile(ctx context.Context, userID string) error {
	if err := s.repo.RestoreCustomer(ctx, userID); err != nil {
		if errors.Is(err, db.ErrAccountNotPendingDeletion) {
			return fmt.Errorf("account not eligible for restore: %w", ErrAccountNotPendingDeletion)
		}
		return fmt.Errorf("failed to restore profile: %w", err)
	}
	return nil
}

func (s *Service) GetAddresses(ctx context.Context, userID string) ([]models.Address, error) {
	addrs, err := s.repo.GetUserAddresses(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user addresses: %w", err)
	}
	return addrs, nil
}

func (s *Service) AddAddress(ctx context.Context, userID string, addr *models.Address) error {
	if err := s.repo.CreateAddress(ctx, userID, addr); err != nil {
		return fmt.Errorf("failed to add address: %w", err)
	}
	if addr.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, userID, addr.AddressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

func (s *Service) UpdateAddress(ctx context.Context, userID, addressID string, req *models.UpdateAddressRequest) error {
	if err := s.repo.UpdateAddress(ctx, userID, addressID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update address: %w", err)
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := s.repo.SetAddressDefault(ctx, userID, addressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

func (s *Service) DeleteAddress(ctx context.Context, userID, addressID string) error {
	if err := s.repo.DeleteAddress(ctx, userID, addressID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete address: %w", err)
	}
	return nil
}

func (s *Service) GetPayments(ctx context.Context, userID string) ([]models.PaymentMethod, error) {
	methods, err := s.repo.ListPayments(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get payment methods: %w", err)
	}
	return methods, nil
}

func (s *Service) UpsertPayment(ctx context.Context, userID string, pm *models.PaymentMethod) error {
	if err := s.repo.UpsertPayment(ctx, userID, pm); err != nil {
		return fmt.Errorf("failed to upsert payment method: %w", err)
	}
	if pm.IsDefault {
		if err := s.repo.SetPaymentDefault(ctx, userID, pm.PaymentID); err != nil {
			return fmt.Errorf("failed to enforce default payment method: %w", err)
		}
	}
	return nil
}

func (s *Service) DeletePayment(ctx context.Context, userID, paymentID string) error {
	if err := s.repo.DeletePayment(ctx, userID, paymentID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("payment method not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete payment method: %w", err)
	}
	return nil
}

func (s *Service) GetPreferences(ctx context.Context, userID string) (*models.Preferences, error) {
	prefs, err := s.repo.GetUserPreferences(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user preferences: %w", err)
	}
	return prefs, nil
}

func (s *Service) UpdatePreferences(ctx context.Context, userID string, req *models.UpdatePreferencesRequest) error {
	if req.Communication != nil {
		for k := range req.Communication {
			if !models.ValidCommunicationChannels[k] {
				return fmt.Errorf("invalid communication channel %q: %w", k, ErrInvalidCommunicationChannel)
			}
		}
	}
	if err := s.repo.UpdateUserPreferences(ctx, userID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update user preferences: %w", err)
	}
	return nil
}

func (s *Service) DeletePreferences(ctx context.Context, userID string) error {
	if err := s.repo.DeleteUserPreferences(ctx, userID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete user preferences: %w", err)
	}
	return nil
}

func (s *Service) GetCredentials(ctx context.Context, email string) (*models.CredentialsResponse, error) {
	if creds, ok := s.credentialsCache.Get(email); ok {
		return creds, nil
	}
	creds, err := s.repo.GetUserCredentialsByEmail(ctx, email)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}
	s.credentialsCache.Set(email, creds, 60*time.Second)
	return creds, nil
}

func (s *Service) UpdateCredentials(ctx context.Context, userID string, req *models.UpdateCredentialsRequest) error {
	if userID == "" {
		return fmt.Errorf("invalid credentials update: %w", errors.New("customer_id is required"))
	}
	if req.PasswordHash == "" && len(req.AuthMethods) == 0 {
		return fmt.Errorf("invalid credentials update: %w", errors.New("at least one of password_hash or auth_methods is required"))
	}
	if err := s.repo.UpdateUserCredentials(ctx, userID, req); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update credentials: %w", err)
	}
	return nil
}

func (s *Service) CheckUserExists(ctx context.Context, email string) (*models.UserExistsResponse, error) {
	result, err := s.repo.GetUserExistsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check if user exists: %w", err)
	}
	return result, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, db.ErrNotFound)
}

func (s *Service) GetPasskeys(ctx context.Context, userID string) ([]models.PasskeyCredential, error) {
	creds, err := s.repo.GetUserPasskeys(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user passkeys: %w", err)
	}
	return creds, nil
}

func (s *Service) AddPasskey(ctx context.Context, userID string, cred *models.PasskeyCredential) error {
	if err := s.repo.CreatePasskey(ctx, userID, cred); err != nil {
		if errors.Is(err, db.ErrPasskeyAlreadyExists) {
			return fmt.Errorf("passkey already exists: %w", ErrPasskeyAlreadyExists)
		}
		return fmt.Errorf("failed to add passkey: %w", err)
	}
	return nil
}

func (s *Service) UpdatePasskey(ctx context.Context, userID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error) {
	cred, err := s.repo.UpdatePasskey(ctx, userID, credentialID, update)
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

func (s *Service) DeletePasskey(ctx context.Context, userID, credentialID string) error {
	if err := s.repo.DeletePasskey(ctx, userID, credentialID); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("passkey not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to delete passkey: %w", err)
	}
	return nil
}

func (s *Service) GetSettings(ctx context.Context, customerID string) (*models.AccountSettings, error) {
	settings, err := s.repo.GetSettings(ctx, customerID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("settings not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	return settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, customerID string, req *models.UpdateSettingsRequest) (*models.AccountSettings, error) {
	if req.Status != nil {
		if err := validateStatus(*req.Status); err != nil {
			return nil, err
		}
	}
	if err := s.repo.UpdateSettingsPartial(ctx, customerID, req, req.Version); err != nil {
		if errors.Is(err, db.ErrVersionConflict) {
			return nil, fmt.Errorf("version conflict: %w", ErrVersionConflict)
		}
		if isNotFound(err) {
			return nil, fmt.Errorf("customer not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update settings: %w", err)
	}
	settings, err := s.repo.GetSettings(ctx, customerID)
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
	"customer-api":           "system.",
}

var tagRE = regexp.MustCompile(`^[a-z0-9._]{1,32}$`)

func (s *Service) UpdateSettingsTags(ctx context.Context, customerID, callerService string, req *models.UpdateSettingsTagsRequest) (*models.AccountSettings, error) {
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

	current, err := s.repo.GetSettings(ctx, customerID)
	if err != nil {
		if !isNotFound(err) {
			return nil, fmt.Errorf("failed to load settings for tag mutation: %w", err)
		}
		defaults := &models.AccountSettings{Status: "active"}
		if createErr := s.repo.UpdateSettings(ctx, customerID, defaults); createErr != nil {
			return nil, fmt.Errorf("failed to initialize settings for tag mutation: %w", createErr)
		}
		current = defaults
	}

	if expectedTagCount(current.Tags, req.Add, req.Remove) > 20 {
		return nil, fmt.Errorf("tag limit exceeded (max 20): %w", ErrForbiddenNamespace)
	}

	if err := s.repo.MutateSettingsTags(ctx, customerID, req, req.Version); err != nil {
		if errors.Is(err, db.ErrVersionConflict) {
			return nil, fmt.Errorf("version conflict: %w", ErrVersionConflict)
		}
		return nil, fmt.Errorf("failed to update tags: %w", err)
	}
	settings, err := s.repo.GetSettings(ctx, customerID)
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

func (s *Service) gatherProfileExportData(ctx context.Context, customerID string) (*profileExportData, error) {
	d := &profileExportData{}
	var err error

	d.settings, err = s.repo.GetSettings(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get settings for export: %w", err)
	}

	d.prefs, err = s.repo.GetUserPreferences(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get preferences for export: %w", err)
	}

	d.addrs, err = s.repo.GetUserAddresses(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get addresses for export: %w", err)
	}

	d.payments, err = s.repo.ListPayments(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get payments for export: %w", err)
	}

	d.consentHistory, err = s.repo.ListConsentHistory(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get consent history for export: %w", err)
	}

	d.passkeys, err = s.repo.GetUserPasskeys(ctx, customerID)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("failed to get passkeys for export: %w", err)
	}

	return d, nil
}

func (s *Service) ExportProfile(ctx context.Context, customerID string) (*models.ExportProfileResponse, error) {
	if s.s3Ops == nil {
		return nil, fmt.Errorf("export not configured")
	}

	profile, err := s.repo.GetUser(ctx, customerID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user profile for export: %w", err)
	}

	d, err := s.gatherProfileExportData(ctx, customerID)
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
	key := "exports/" + customerID + "/" + exportID + ".json"

	if _, err := s.s3Ops.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(s.exportBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	}); err != nil {
		return nil, fmt.Errorf("failed to write export to s3: %w", err)
	}

	presigned, err := s.s3Presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.exportBucket),
		Key:    aws.String(key),
	}, func(opts *awss3.PresignOptions) {
		opts.Expires = 15 * time.Minute
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate export download url: %w", err)
	}

	return &models.ExportProfileResponse{
		ExportID:    exportID,
		DownloadURL: presigned.URL,
		ExpiresAt:   time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339),
	}, nil
}

func (s *Service) deleteS3Exports(ctx context.Context, customerID string) error {
	if s.s3Ops == nil {
		return nil
	}
	prefix := "exports/" + customerID + "/"
	listOut, err := s.s3Ops.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
		Bucket: aws.String(s.exportBucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		logger.Error("failed to list s3 exports for erasure", err, logger.Attr("customer_id", customerID))
		return fmt.Errorf("failed to list s3 exports for erasure: %w", err)
	}
	if len(listOut.Contents) == 0 {
		return nil
	}
	objs := make([]s3types.ObjectIdentifier, len(listOut.Contents))
	for i, obj := range listOut.Contents {
		objs[i] = s3types.ObjectIdentifier{Key: obj.Key}
	}
	if _, err := s.s3Ops.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
		Bucket: aws.String(s.exportBucket),
		Delete: &s3types.Delete{Objects: objs},
	}); err != nil {
		logger.Error("failed to delete s3 exports for erasure", err, logger.Attr("customer_id", customerID))
		return fmt.Errorf("failed to delete s3 exports for erasure: %w", err)
	}
	return nil
}

type unsubPayload struct {
	CustomerID string `json:"customer_id"`
	Channel    string `json:"channel"`
	Exp        int64  `json:"exp"`
	JTI        string `json:"jti"`
}

func (s *Service) MintUnsubscribeToken(ctx context.Context, customerID, channel string) (string, error) {
	if len(s.unsubscribeKey) == 0 {
		return "", fmt.Errorf("unsubscribe key not configured")
	}
	if !models.ValidCommunicationChannels[channel] {
		return "", fmt.Errorf("invalid unsubscribe channel %q: %w", channel, ErrInvalidUnsubscribeChannel)
	}
	if _, err := s.repo.GetUser(ctx, customerID); err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("customer not found: %w", ErrNotFound)
		}
		return "", fmt.Errorf("failed to verify customer for token mint: %w", err)
	}
	payload, err := json.Marshal(unsubPayload{
		CustomerID: customerID,
		Channel:    channel,
		Exp:        time.Now().Add(30 * 24 * time.Hour).Unix(),
		JTI:        ksuid.New().String(),
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

func (s *Service) GetAvatarUploadURL(ctx context.Context, customerID string) (string, error) {
	if s.avatarPresign == nil {
		return "", fmt.Errorf("avatar upload not configured")
	}
	key := "avatars/" + customerID + "/avatar"
	url, err := s.avatarPresign.PresignPut(ctx, s.avatarBucket, key, 15*time.Minute, "", 0)
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

	existing, err := s.repo.GetLatestConsent(ctx, p.CustomerID, p.Channel)
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
	if err := s.repo.AppendConsentLog(ctx, p.CustomerID, entry); err != nil {
		return fmt.Errorf("failed to record unsubscribe: %w", err)
	}
	return nil
}
