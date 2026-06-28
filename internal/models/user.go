package models

import (
	"time"
)

type User struct {
	CustomerID    string    `json:"customer_id"              dynamodbav:"customer_id"`
	Username      string    `json:"username,omitempty"       dynamodbav:"username,omitempty"`
	Email         string    `json:"email"                    dynamodbav:"email"`
	Phone         string    `json:"phone,omitempty"          dynamodbav:"phone,omitempty"`
	FirstName     string    `json:"first_name"               dynamodbav:"first_name"`
	MiddleInitial string    `json:"middle_initial,omitempty" dynamodbav:"middle_initial,omitempty"`
	LastName      string    `json:"last_name"                dynamodbav:"last_name"`
	AvatarURL     string    `json:"avatar_url,omitempty"     dynamodbav:"avatar_url,omitempty"`
	PasswordHash  string    `json:"-"                        dynamodbav:"password_hash"`
	EmailVerified bool      `json:"-"                        dynamodbav:"email_verified"`
	AuthMethods   []string  `json:"-"                        dynamodbav:"auth_methods"`
	CreatedAt     time.Time `json:"-"                        dynamodbav:"created_at"`
	UpdatedAt     time.Time `json:"-"                        dynamodbav:"updated_at"`
}

type Address struct {
	AddressID string `json:"address_id"      dynamodbav:"address_id"`
	Alias     string `json:"alias,omitempty" dynamodbav:"alias,omitempty"`
	Line1     string `json:"line1"           dynamodbav:"line1"`
	Line2     string `json:"line2,omitempty" dynamodbav:"line2,omitempty"`
	City      string `json:"city"            dynamodbav:"city"`
	State     string `json:"state"           dynamodbav:"state"`
	ZipCode   string `json:"zip_code"        dynamodbav:"zip_code"`
	Country   string `json:"country"         dynamodbav:"country"`
	IsDefault bool   `json:"is_default"      dynamodbav:"is_default"`
}

type PaymentMethod struct {
	PaymentID   string `json:"payment_id"   dynamodbav:"payment_id"`
	Provider    string `json:"provider"     dynamodbav:"provider"`
	Token       string `json:"-"            dynamodbav:"token"`
	Last4       string `json:"last4"        dynamodbav:"last4"`
	Brand       string `json:"brand"        dynamodbav:"brand"`
	ExpiryMonth int    `json:"expiry_month" dynamodbav:"expiry_month"`
	ExpiryYear  int    `json:"expiry_year"  dynamodbav:"expiry_year"`
	IsDefault   bool   `json:"is_default"   dynamodbav:"is_default"`
}

type UpdateCredentialsRequest struct {
	PasswordHash string   `json:"password_hash"`
	AuthMethods  []string `json:"auth_methods"`
}

type CredentialsResponse struct {
	CustomerID    string   `json:"customer_id"`
	PasswordHash  string   `json:"password_hash"`
	EmailVerified bool     `json:"email_verified"`
	AuthMethods   []string `json:"auth_methods"`
}

type UserExistsResponse struct {
	Exists      bool     `json:"exists"`
	AuthMethods []string `json:"auth_methods"`
}

type PasskeyCredential struct {
	CredentialID   string     `json:"credential_id"             dynamodbav:"credential_id"`
	PublicKey      string     `json:"public_key"                dynamodbav:"public_key"`
	SignCount      uint32     `json:"sign_count"                dynamodbav:"sign_count"`
	Transports     []string   `json:"transports,omitempty"      dynamodbav:"transports,omitempty"`
	AAGUID         string     `json:"aaguid,omitempty"          dynamodbav:"aaguid,omitempty"`
	BackupEligible bool       `json:"backup_eligible"           dynamodbav:"backup_eligible"`
	BackupState    bool       `json:"backup_state"              dynamodbav:"backup_state"`
	CreatedAt      time.Time  `json:"created_at"                dynamodbav:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"    dynamodbav:"last_used_at,omitempty"`
}

type Preferences struct {
	Language      string            `json:"language"                  dynamodbav:"language"`
	Timezone      string            `json:"timezone"                  dynamodbav:"timezone"`
	Communication map[string]bool   `json:"communication"             dynamodbav:"communication"`
	Marketing     map[string]string `json:"marketing"                 dynamodbav:"marketing"`
}

type AccountSettings struct {
	EmailVerified   bool              `json:"email_verified"              dynamodbav:"email_verified"`
	EmailVerifiedAt *time.Time        `json:"email_verified_at,omitempty" dynamodbav:"email_verified_at,omitempty"`
	PhoneVerified   bool              `json:"phone_verified"              dynamodbav:"phone_verified"`
	PhoneVerifiedAt *time.Time        `json:"phone_verified_at,omitempty" dynamodbav:"phone_verified_at,omitempty"`
	Status          string            `json:"status"                      dynamodbav:"status"`
	StatusReason    string            `json:"status_reason,omitempty"     dynamodbav:"status_reason,omitempty"`
	StatusChangedAt *time.Time        `json:"status_changed_at,omitempty" dynamodbav:"status_changed_at,omitempty"`
	Tags            []string          `json:"tags,omitempty"              dynamodbav:"tags,omitempty"`
	Segments        map[string]string `json:"segments,omitempty"          dynamodbav:"segments,omitempty"`
}

type ConsentLog struct {
	Channel    string    `json:"channel"               dynamodbav:"channel"`
	Action     string    `json:"action"                dynamodbav:"action"`
	Source     string    `json:"source"                dynamodbav:"source"`
	SourceRef  string    `json:"source_ref,omitempty"  dynamodbav:"source_ref,omitempty"`
	IPAddress  string    `json:"ip_address,omitempty"  dynamodbav:"ip_address,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"  dynamodbav:"user_agent,omitempty"`
	RecordedAt time.Time `json:"recorded_at"           dynamodbav:"recorded_at"`
}

type UpdateSettingsTagsRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type PasskeyExport struct {
	CredentialID   string     `json:"credential_id"`
	SignCount      uint32     `json:"sign_count"`
	Transports     []string   `json:"transports,omitempty"`
	AAGUID         string     `json:"aaguid,omitempty"`
	BackupEligible bool       `json:"backup_eligible"`
	BackupState    bool       `json:"backup_state"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
}

type ProfileExport struct {
	Profile        *User            `json:"profile"`
	Settings       *AccountSettings `json:"settings"`
	Preferences    *Preferences     `json:"preferences"`
	Addresses      []Address        `json:"addresses"`
	Payments       []PaymentMethod  `json:"payments"`
	ConsentHistory []ConsentLog     `json:"consent_history"`
	Passkeys       []PasskeyExport  `json:"passkeys"`
}

type ExportProfileResponse struct {
	ExportID    string `json:"export_id"`
	DownloadURL string `json:"download_url"`
	ExpiresAt   string `json:"expires_at"`
}

type MintUnsubscribeTokenRequest struct {
	Channel string `json:"channel"`
}

type MintUnsubscribeTokenResponse struct {
	Token string `json:"token"`
}

type UnsubscribeRequest struct {
	Token string `json:"token"`
}
