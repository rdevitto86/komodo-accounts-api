package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"komodo-user-api/internal/db"
	"komodo-user-api/internal/models"
)

var ErrNotFound = errors.New("not found")
var ErrAlreadyExists = errors.New("already exists")

func (s *Service) GetProfile(ctx context.Context, userID string) (*models.User, error) {
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	return user, nil
}

func (s *Service) CreateUser(ctx context.Context, user *models.User) error {
	if user.UserID == "" || user.Email == "" || user.FirstName == "" || user.LastName == "" {
		return fmt.Errorf("invalid user: %w", errors.New("user_id, email, first_name, and last_name are required"))
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
	return updated, nil
}

func (s *Service) DeleteProfile(ctx context.Context, userID string) error {
	if err := s.repo.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete user profile: %w", err)
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
	creds, err := s.repo.GetUserCredentialsByEmail(ctx, email)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}
	return creds, nil
}

func (s *Service) UpdateCredentials(ctx context.Context, userID string, req *models.UpdateCredentialsRequest) error {
	if userID == "" {
		return fmt.Errorf("invalid credentials update: %w", errors.New("user_id is required"))
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
		if errors.Is(err, db.ErrAlreadyExists) {
			return fmt.Errorf("passkey already exists: %w", ErrAlreadyExists)
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
