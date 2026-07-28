package db

import (
	"time"

	"komodo-accounts-api/internal/models"
)

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefSlice[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}

func ptrSlice[T any](s []T) *[]T {
	if len(s) == 0 {
		return nil
	}
	return &s
}

func optMap[K comparable, V any](m map[K]V) *map[K]V {
	if len(m) == 0 {
		return nil
	}
	return &m
}

func toAuthMethods[T ~string](in []string) []T {
	out := make([]T, len(in))
	for i, s := range in {
		out[i] = T(s)
	}
	return out
}

type addressRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	AddressID string `dynamodbav:"address_id"`
	Alias     string `dynamodbav:"alias,omitempty"`
	Line1     string `dynamodbav:"line1"`
	Line2     string `dynamodbav:"line2,omitempty"`
	City      string `dynamodbav:"city"`
	State     string `dynamodbav:"state"`
	ZipCode   string `dynamodbav:"zip_code"`
	Country   string `dynamodbav:"country"`
	IsDefault bool   `dynamodbav:"is_default"`
}

func (r *addressRecord) toModel() models.Address {
	return models.Address{
		AddressId: r.AddressID,
		Alias:     optStr(r.Alias),
		Line1:     r.Line1,
		Line2:     optStr(r.Line2),
		City:      r.City,
		State:     r.State,
		ZipCode:   r.ZipCode,
		Country:   r.Country,
		IsDefault: r.IsDefault,
	}
}

func addressRecordFrom(pk, sk string, a *models.Address) addressRecord {
	return addressRecord{
		PK:        pk,
		SK:        sk,
		AddressID: a.AddressId,
		Alias:     deref(a.Alias),
		Line1:     a.Line1,
		Line2:     deref(a.Line2),
		City:      a.City,
		State:     a.State,
		ZipCode:   a.ZipCode,
		Country:   a.Country,
		IsDefault: a.IsDefault,
	}
}

type paymentRecord struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	PaymentID   string `dynamodbav:"payment_id"`
	Provider    string `dynamodbav:"provider"`
	Last4       string `dynamodbav:"last4"`
	Brand       string `dynamodbav:"brand"`
	ExpiryMonth int    `dynamodbav:"expiry_month"`
	ExpiryYear  int    `dynamodbav:"expiry_year"`
	IsDefault   bool   `dynamodbav:"is_default"`
}

func (r *paymentRecord) toModel() models.PaymentMethod {
	return models.PaymentMethod{
		PaymentId:   r.PaymentID,
		Provider:    r.Provider,
		Last4:       r.Last4,
		Brand:       r.Brand,
		ExpiryMonth: r.ExpiryMonth,
		ExpiryYear:  r.ExpiryYear,
		IsDefault:   r.IsDefault,
	}
}

func paymentRecordFrom(pk, sk string, m *models.PaymentMethod) paymentRecord {
	return paymentRecord{
		PK:          pk,
		SK:          sk,
		PaymentID:   m.PaymentId,
		Provider:    m.Provider,
		Last4:       m.Last4,
		Brand:       m.Brand,
		ExpiryMonth: m.ExpiryMonth,
		ExpiryYear:  m.ExpiryYear,
		IsDefault:   m.IsDefault,
	}
}

type prefsRecord struct {
	PK            string          `dynamodbav:"PK"`
	SK            string          `dynamodbav:"SK"`
	Language      string          `dynamodbav:"language"`
	Timezone      string          `dynamodbav:"timezone"`
	Communication map[string]bool `dynamodbav:"communication"`
}

func (r *prefsRecord) toModel() models.Preferences {
	return models.Preferences{
		Language:      optStr(r.Language),
		Timezone:      optStr(r.Timezone),
		Communication: optMap(r.Communication),
	}
}

type passkeyRecord struct {
	PK             string     `dynamodbav:"PK"`
	SK             string     `dynamodbav:"SK"`
	CredentialID   string     `dynamodbav:"credential_id"`
	PublicKey      string     `dynamodbav:"public_key"`
	SignCount      int64      `dynamodbav:"sign_count"`
	Transports     []string   `dynamodbav:"transports,omitempty"`
	AAGUID         string     `dynamodbav:"aaguid,omitempty"`
	BackupEligible bool       `dynamodbav:"backup_eligible"`
	BackupState    bool       `dynamodbav:"backup_state"`
	CreatedAt      time.Time  `dynamodbav:"created_at"`
	LastUsedAt     *time.Time `dynamodbav:"last_used_at,omitempty"`
}

func (r *passkeyRecord) toModel() models.PasskeyCredential {
	return models.PasskeyCredential{
		CredentialId:   r.CredentialID,
		PublicKey:      r.PublicKey,
		SignCount:      r.SignCount,
		Transports:     ptrSlice(r.Transports),
		Aaguid:         optStr(r.AAGUID),
		BackupEligible: r.BackupEligible,
		BackupState:    r.BackupState,
		CreatedAt:      r.CreatedAt,
		LastUsedAt:     r.LastUsedAt,
	}
}

func passkeyRecordFrom(pk, sk string, c *models.PasskeyCredential) passkeyRecord {
	return passkeyRecord{
		PK:             pk,
		SK:             sk,
		CredentialID:   c.CredentialId,
		PublicKey:      c.PublicKey,
		SignCount:      c.SignCount,
		Transports:     derefSlice(c.Transports),
		AAGUID:         deref(c.Aaguid),
		BackupEligible: c.BackupEligible,
		BackupState:    c.BackupState,
		CreatedAt:      c.CreatedAt,
		LastUsedAt:     c.LastUsedAt,
	}
}

type settingsRecord struct {
	PK              string     `dynamodbav:"PK"`
	SK              string     `dynamodbav:"SK"`
	EmailVerified   bool       `dynamodbav:"email_verified"`
	EmailVerifiedAt *time.Time `dynamodbav:"email_verified_at,omitempty"`
	PhoneVerified   bool       `dynamodbav:"phone_verified"`
	PhoneVerifiedAt *time.Time `dynamodbav:"phone_verified_at,omitempty"`
	Status          string     `dynamodbav:"status"`
	StatusReason    string     `dynamodbav:"status_reason,omitempty"`
	StatusChangedAt *time.Time `dynamodbav:"status_changed_at,omitempty"`
	Tags            []string   `dynamodbav:"tags,omitempty,stringset"`
	Version         int        `dynamodbav:"version"`
}

func (r *settingsRecord) toModel() models.AccountSettings {
	return models.AccountSettings{
		EmailVerified:   &r.EmailVerified,
		EmailVerifiedAt: r.EmailVerifiedAt,
		PhoneVerified:   &r.PhoneVerified,
		PhoneVerifiedAt: r.PhoneVerifiedAt,
		Status:          models.AccountSettingsStatus(r.Status),
		StatusReason:    optStr(r.StatusReason),
		StatusChangedAt: r.StatusChangedAt,
		Tags:            ptrSlice(r.Tags),
		Version:         r.Version,
	}
}

func settingsRecordFrom(pk, sk string, s *models.AccountSettings) settingsRecord {
	return settingsRecord{
		PK:              pk,
		SK:              sk,
		EmailVerified:   deref(s.EmailVerified),
		EmailVerifiedAt: s.EmailVerifiedAt,
		PhoneVerified:   deref(s.PhoneVerified),
		PhoneVerifiedAt: s.PhoneVerifiedAt,
		Status:          string(s.Status),
		StatusReason:    deref(s.StatusReason),
		StatusChangedAt: s.StatusChangedAt,
		Tags:            derefSlice(s.Tags),
		Version:         s.Version,
	}
}

type consentRecord struct {
	PK         string    `dynamodbav:"PK"`
	SK         string    `dynamodbav:"SK"`
	Channel    string    `dynamodbav:"channel"`
	Action     string    `dynamodbav:"action"`
	Source     string    `dynamodbav:"source"`
	SourceRef  string    `dynamodbav:"source_ref,omitempty"`
	IPAddress  string    `dynamodbav:"ip_address,omitempty"`
	UserAgent  string    `dynamodbav:"user_agent,omitempty"`
	RecordedAt time.Time `dynamodbav:"recorded_at"`
}

func (r *consentRecord) toModel() models.ConsentLog {
	return models.ConsentLog{
		Channel:    models.ConsentLogChannel(r.Channel),
		Action:     models.ConsentLogAction(r.Action),
		Source:     r.Source,
		SourceRef:  optStr(r.SourceRef),
		IpAddress:  optStr(r.IPAddress),
		UserAgent:  optStr(r.UserAgent),
		RecordedAt: r.RecordedAt,
	}
}

func consentRecordFrom(pk, sk string, c *models.ConsentLog) consentRecord {
	return consentRecord{
		PK:         pk,
		SK:         sk,
		Channel:    string(c.Channel),
		Action:     string(c.Action),
		Source:     c.Source,
		SourceRef:  deref(c.SourceRef),
		IPAddress:  deref(c.IpAddress),
		UserAgent:  deref(c.UserAgent),
		RecordedAt: c.RecordedAt,
	}
}
