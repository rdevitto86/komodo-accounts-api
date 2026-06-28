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
	"sort"
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
var ErrForbiddenNamespace = errors.New("forbidden namespace")
var ErrMarketingConsentMismatch = errors.New("marketing consent mismatch")

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
	user.EmailVerified = false
	if user.AuthMethods == nil {
		user.AuthMethods = []string{}
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
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
	if err := s.repo.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete user profile: %w", err)
	}
	s.profileCache.Delete(userID)
	s.deleteS3Exports(ctx, userID)
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
		if err := s.demoteOtherDefaultAddresses(ctx, userID, addr.AddressID); err != nil {
			return fmt.Errorf("failed to enforce default address: %w", err)
		}
	}
	return nil
}

func (s *Service) UpdateAddress(ctx context.Context, userID, addressID string, update *models.Address) error {
	update.AddressID = addressID
	if err := s.repo.UpdateAddress(ctx, userID, *update); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("address not found: %w", ErrNotFound)
		}
		return fmt.Errorf("failed to update address: %w", err)
	}
	if update.IsDefault {
		if err := s.demoteOtherDefaultAddresses(ctx, userID, addressID); err != nil {
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
		if err := s.demoteOtherDefaultPayments(ctx, userID, pm.PaymentID); err != nil {
			return fmt.Errorf("failed to enforce default payment method: %w", err)
		}
	}
	return nil
}

func (s *Service) demoteOtherDefaultAddresses(ctx context.Context, userID, keepID string) error {
	addrs, err := s.repo.GetUserAddresses(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to list addresses for default enforcement: %w", err)
	}
	for i := range addrs {
		if addrs[i].AddressID == keepID || !addrs[i].IsDefault {
			continue
		}
		if err := s.repo.SetAddressDefault(ctx, userID, addrs[i].AddressID, false); err != nil {
			return fmt.Errorf("failed to demote default address: %w", err)
		}
	}
	return nil
}

func (s *Service) demoteOtherDefaultPayments(ctx context.Context, userID, keepID string) error {
	methods, err := s.repo.ListPayments(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to list payment methods for default enforcement: %w", err)
	}
	for i := range methods {
		if methods[i].PaymentID == keepID || !methods[i].IsDefault {
			continue
		}
		if err := s.repo.SetPaymentDefault(ctx, userID, methods[i].PaymentID, false); err != nil {
			return fmt.Errorf("failed to demote default payment method: %w", err)
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

func (s *Service) UpdatePreferences(ctx context.Context, userID string, prefs *models.Preferences) error {
	if len(prefs.Marketing) > 0 {
		return fmt.Errorf("marketing consent must go through consent log: %w", ErrMarketingConsentMismatch)
	}
	if err := s.repo.UpdateUserPreferences(ctx, userID, prefs); err != nil {
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
	return errors.Is(err, db.ErrNotFound) ||
		strings.Contains(err.Error(), "ResourceNotFoundException")
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

func (s *Service) UpdateSettings(ctx context.Context, customerID string, settings *models.AccountSettings) (*models.AccountSettings, error) {
	if settings.Status != "" {
		if err := validateStatus(settings.Status); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		settings.StatusChangedAt = &now
	}
	if err := s.repo.UpdateSettings(ctx, customerID, settings); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("customer not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to update settings: %w", err)
	}
	return settings, nil
}

func validateStatus(s string) error {
	switch s {
	case "active", "suspended", "closed", "pending_deletion":
		return nil
	}
	return fmt.Errorf("invalid account status %q", s)
}

var tagNamespaceMap = map[string]string{
	"loyalty-api":            "loyalty.",
	"marketing-api":          "marketing.",
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

	settings, err := s.repo.GetSettings(ctx, customerID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("failed to load settings for tag update: %w", err)
	}
	if settings == nil {
		settings = &models.AccountSettings{Status: "active"}
	}

	removeSet := make(map[string]bool, len(req.Remove))
	for _, t := range req.Remove {
		removeSet[t] = true
	}
	tagSet := make(map[string]bool)
	for _, t := range settings.Tags {
		if !removeSet[t] {
			tagSet[t] = true
		}
	}
	for _, t := range req.Add {
		tagSet[t] = true
	}
	if len(tagSet) > 20 {
		return nil, fmt.Errorf("tag limit exceeded (max 20): %w", ErrForbiddenNamespace)
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	settings.Tags = tags

	if err := s.repo.UpdateSettings(ctx, customerID, settings); err != nil {
		return nil, fmt.Errorf("failed to update tags: %w", err)
	}
	return settings, nil
}

func (s *Service) ExportProfile(ctx context.Context, customerID string) (*models.ExportProfileResponse, error) {
	if s.s3Client == nil {
		return nil, fmt.Errorf("export not configured")
	}

	profile, _ := s.repo.GetUser(ctx, customerID)
	settings, _ := s.repo.GetSettings(ctx, customerID)
	prefs, _ := s.repo.GetUserPreferences(ctx, customerID)
	addrs, _ := s.repo.GetUserAddresses(ctx, customerID)
	payments, _ := s.repo.ListPayments(ctx, customerID)
	consentHistory, _ := s.repo.ListConsentHistory(ctx, customerID)
	passkeys, _ := s.repo.GetUserPasskeys(ctx, customerID)

	passkeyExports := make([]models.PasskeyExport, len(passkeys))
	for i, pk := range passkeys {
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
		Settings:       settings,
		Preferences:    prefs,
		Addresses:      addrs,
		Payments:       payments,
		ConsentHistory: consentHistory,
		Passkeys:       passkeyExports,
	}

	data, err := json.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal export: %w", err)
	}

	exportID := ksuid.New().String()
	key := "exports/" + customerID + "/" + exportID + ".json"

	if _, err := s.s3Client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(s.exportBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	}); err != nil {
		return nil, fmt.Errorf("failed to write export to s3: %w", err)
	}

	presigner := awss3.NewPresignClient(s.s3Client)
	presigned, err := presigner.PresignGetObject(ctx, &awss3.GetObjectInput{
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

func (s *Service) deleteS3Exports(ctx context.Context, customerID string) {
	if s.s3Client == nil {
		return
	}
	prefix := "exports/" + customerID + "/"
	listOut, err := s.s3Client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
		Bucket: aws.String(s.exportBucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		logger.Warn("failed to list s3 exports for erasure", err, logger.Attr("customer_id", customerID))
		return
	}
	if len(listOut.Contents) == 0 {
		return
	}
	objs := make([]s3types.ObjectIdentifier, len(listOut.Contents))
	for i, obj := range listOut.Contents {
		objs[i] = s3types.ObjectIdentifier{Key: obj.Key}
	}
	if _, err := s.s3Client.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
		Bucket: aws.String(s.exportBucket),
		Delete: &s3types.Delete{Objects: objs},
	}); err != nil {
		logger.Warn("failed to delete s3 exports for erasure", err, logger.Attr("customer_id", customerID))
	}
}

type unsubPayload struct {
	CustomerID string `json:"customer_id"`
	Channel    string `json:"channel"`
	Exp        int64  `json:"exp"`
}

func (s *Service) MintUnsubscribeToken(ctx context.Context, customerID, channel string) (string, error) {
	if len(s.unsubscribeKey) == 0 {
		return "", fmt.Errorf("unsubscribe key not configured")
	}
	payload, err := json.Marshal(unsubPayload{
		CustomerID: customerID,
		Channel:    channel,
		Exp:        time.Now().Add(30 * 24 * time.Hour).Unix(),
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

func (s *Service) VerifyAndRecordUnsubscribe(ctx context.Context, token, ipAddr, userAgent string) error {
	if len(s.unsubscribeKey) == 0 {
		return fmt.Errorf("unsubscribe key not configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) < 32 {
		return fmt.Errorf("invalid unsubscribe token: %w", ErrNotFound)
	}
	payloadBytes := raw[:len(raw)-32]
	sig := raw[len(raw)-32:]

	mac := hmac.New(sha256.New, s.unsubscribeKey)
	mac.Write(payloadBytes)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return fmt.Errorf("invalid unsubscribe token signature: %w", ErrNotFound)
	}

	var p unsubPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return fmt.Errorf("failed to parse unsubscribe payload: %w", ErrNotFound)
	}
	if time.Now().Unix() > p.Exp {
		return fmt.Errorf("unsubscribe token expired: %w", ErrNotFound)
	}

	entry := &models.ConsentLog{
		Channel:   p.Channel,
		Action:    "opt_out",
		Source:    "unsubscribe_link",
		IPAddress: ipAddr,
		UserAgent: userAgent,
	}
	if err := s.repo.AppendConsentLog(ctx, p.CustomerID, entry); err != nil {
		return fmt.Errorf("failed to record unsubscribe: %w", err)
	}
	return nil
}
