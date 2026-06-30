//go:generate go run go.uber.org/mock/mockgen -source=repo.go -destination=mocks/mock_repo.go -package=mocks
package api

import (
	"context"

	"komodo-customer-api/internal/models"
)

type repository interface {
	GetUser(ctx context.Context, userID string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, userID string, update *models.User) (*models.User, error)
	DeleteUser(ctx context.Context, userID string) error
	SoftDeleteCustomer(ctx context.Context, customerID string) error
	RestoreCustomer(ctx context.Context, customerID string) error

	GetUserAddresses(ctx context.Context, userID string) ([]models.Address, error)
	CreateAddress(ctx context.Context, userID string, addr *models.Address) error
	UpdateAddress(ctx context.Context, userID, addressID string, req *models.UpdateAddressRequest) error
	DeleteAddress(ctx context.Context, userID, addressID string) error
	SetAddressDefault(ctx context.Context, userID, addressID string) error

	ListPayments(ctx context.Context, userID string) ([]models.PaymentMethod, error)
	UpsertPayment(ctx context.Context, userID string, method *models.PaymentMethod) error
	DeletePayment(ctx context.Context, userID, paymentID string) error
	SetPaymentDefault(ctx context.Context, userID, paymentID string) error

	GetUserPreferences(ctx context.Context, userID string) (*models.Preferences, error)
	UpdateUserPreferences(ctx context.Context, userID string, req *models.UpdatePreferencesRequest) error
	DeleteUserPreferences(ctx context.Context, userID string) error

	GetUserCredentialsByEmail(ctx context.Context, email string) (*models.CredentialsResponse, error)
	UpdateUserCredentials(ctx context.Context, userID string, req *models.UpdateCredentialsRequest) error
	GetUserExistsByEmail(ctx context.Context, email string) (*models.UserExistsResponse, error)

	GetUserPasskeys(ctx context.Context, userID string) ([]models.PasskeyCredential, error)
	CreatePasskey(ctx context.Context, userID string, cred *models.PasskeyCredential) error
	UpdatePasskey(ctx context.Context, userID, credentialID string, update *models.PasskeyCredential) (*models.PasskeyCredential, error)
	DeletePasskey(ctx context.Context, userID, credentialID string) error

	GetSettings(ctx context.Context, customerID string) (*models.AccountSettings, error)
	UpdateSettings(ctx context.Context, customerID string, settings *models.AccountSettings) error
	UpdateSettingsPartial(ctx context.Context, customerID string, req *models.UpdateSettingsRequest, version int) error
	MutateSettingsTags(ctx context.Context, customerID string, req *models.UpdateSettingsTagsRequest, version int) error

	AppendConsentLog(ctx context.Context, customerID string, entry *models.ConsentLog) error
	ListConsentHistory(ctx context.Context, customerID string) ([]models.ConsentLog, error)
	GetLatestConsent(ctx context.Context, customerID, channel string) (*models.ConsentLog, error)
}
