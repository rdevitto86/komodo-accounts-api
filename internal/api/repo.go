//go:generate go run go.uber.org/mock/mockgen -source=repo.go -destination=../../test/mocks/mock_repo.go -package=mocks
package api

import (
	"context"

	"komodo-accounts-api/internal/models"
)

type repository interface {
	GetAccount(ctx context.Context, accountID string) (*models.Account, error)
	CreateAccount(ctx context.Context, account *models.Account) error
	UpdateAccount(ctx context.Context, accountID string, update *models.UpdateProfileRequest) (*models.Account, error)
	DeleteAccount(ctx context.Context, accountID string) error
	SoftDeleteAccount(ctx context.Context, accountID string) error
	RestoreAccount(ctx context.Context, accountID string) error

	GetAccountAddresses(ctx context.Context, accountID string) ([]models.Address, error)
	CreateAddress(ctx context.Context, accountID string, addr *models.Address) error
	UpdateAddress(ctx context.Context, accountID, addressID string, req *models.UpdateAddressRequest) error
	DeleteAddress(ctx context.Context, accountID, addressID string) error
	SetAddressDefault(ctx context.Context, accountID, addressID string) error

	ListPayments(ctx context.Context, accountID string) ([]models.PaymentMethod, error)
	UpsertPayment(ctx context.Context, accountID string, method *models.PaymentMethod) error
	DeletePayment(ctx context.Context, accountID, paymentID string) error
	SetPaymentDefault(ctx context.Context, accountID, paymentID string) error

	GetAccountPreferences(ctx context.Context, accountID string) (*models.Preferences, error)
	UpdateAccountPreferences(ctx context.Context, accountID string, req *models.UpdatePreferencesRequest) error
	DeleteAccountPreferences(ctx context.Context, accountID string) error

	GetAccountCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error)
	UpdateAccountCredentials(ctx context.Context, accountID string, req *models.UpdateCredentialsRequest) error
	GetAccountExistsByEmail(ctx context.Context, email string) (*models.AccountExistsResponse, error)

	GetAccountPasskeys(ctx context.Context, accountID string) ([]models.PasskeyCredential, error)
	CreatePasskey(ctx context.Context, accountID string, cred *models.PasskeyCredential) error
	UpdatePasskey(ctx context.Context, accountID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error)
	DeletePasskey(ctx context.Context, accountID, credentialID string) error

	GetSettings(ctx context.Context, accountID string) (*models.AccountSettings, error)
	UpdateSettings(ctx context.Context, accountID string, settings *models.AccountSettings) error
	UpdateSettingsPartial(ctx context.Context, accountID string, req *models.UpdateSettingsRequest, version int) error
	MutateSettingsTags(ctx context.Context, accountID string, req *models.UpdateSettingsTagsRequest, version int) error

	AppendConsentLog(ctx context.Context, accountID string, entry *models.ConsentLog) error
	ListConsentHistory(ctx context.Context, accountID string) ([]models.ConsentLog, error)
	GetLatestConsent(ctx context.Context, accountID, channel string) (*models.ConsentLog, error)
}
