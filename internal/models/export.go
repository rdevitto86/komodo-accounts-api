package models

import "time"

type PasskeyExport struct {
	CredentialID   string     `json:"credential_id"`
	SignCount      int64      `json:"sign_count"`
	Transports     []string   `json:"transports,omitempty"`
	AAGUID         string     `json:"aaguid,omitempty"`
	BackupEligible bool       `json:"backup_eligible"`
	BackupState    bool       `json:"backup_state"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
}

type ProfileExport struct {
	Profile        *Account         `json:"profile"`
	Settings       *AccountSettings `json:"settings"`
	Preferences    *Preferences     `json:"preferences"`
	Addresses      []Address        `json:"addresses"`
	Payments       []PaymentMethod  `json:"payments"`
	ConsentHistory []ConsentLog     `json:"consent_history"`
	Passkeys       []PasskeyExport  `json:"passkeys"`
}
