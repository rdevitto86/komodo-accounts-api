package events

type AccountRegisteredPayload struct {
	AccountID   string   `json:"account_id"`
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	AuthMethods []string `json:"auth_methods"`
	CreatedAt   string   `json:"created_at"`
}

type AccountDeletedPayload struct {
	AccountID string `json:"account_id"`
}

type AccountProfileUpdatedPayload struct {
	AccountID     string   `json:"account_id"`
	ChangedFields []string `json:"changed_fields"`
	UpdatedAt     string   `json:"updated_at"`
}

type AccountEmailChangedPayload struct {
	AccountID string `json:"account_id"`
	OldEmail  string `json:"old_email"`
	NewEmail  string `json:"new_email"`
}

type AccountPhoneChangedPayload struct {
	AccountID string `json:"account_id"`
	OldPhone  string `json:"old_phone,omitempty"`
	NewPhone  string `json:"new_phone,omitempty"`
}

type AccountConsentChangedPayload struct {
	AccountID  string `json:"account_id"`
	Channel    string `json:"channel"`
	Action     string `json:"action"`
	Source     string `json:"source"`
	SourceRef  string `json:"source_ref,omitempty"`
	RecordedAt string `json:"recorded_at"`
}

type AccountPreferencesUpdatedPayload struct {
	AccountID     string          `json:"account_id"`
	Language      string          `json:"language"`
	Timezone      string          `json:"timezone"`
	Communication map[string]bool `json:"communication"`
	UpdatedAt     string          `json:"updated_at"`
}

type AccountStatusChangedPayload struct {
	AccountID       string `json:"account_id"`
	OldStatus       string `json:"old_status"`
	NewStatus       string `json:"new_status"`
	StatusReason    string `json:"status_reason,omitempty"`
	StatusChangedAt string `json:"status_changed_at"`
}

type AccountTagsChangedPayload struct {
	AccountID string   `json:"account_id"`
	Added     []string `json:"added"`
	Removed   []string `json:"removed"`
}

type AccountPasskeyAddedPayload struct {
	AccountID    string   `json:"account_id"`
	CredentialID string   `json:"credential_id"`
	AAGUID       string   `json:"aaguid"`
	Transports   []string `json:"transports"`
	CreatedAt    string   `json:"created_at"`
}

type AccountPasskeyRemovedPayload struct {
	AccountID    string `json:"account_id"`
	CredentialID string `json:"credential_id"`
}
