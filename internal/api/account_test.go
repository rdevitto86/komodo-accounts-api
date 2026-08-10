package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rdevitto86/komodo-forge-sdk-go/events/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/test/mocks"
)

type fakeEventSink struct {
	mu      sync.Mutex
	events  []domain.Event
	sendErr error
}

func (f *fakeEventSink) Send(_ context.Context, event domain.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.events = append(f.events, event)
	return nil
}

func (f *fakeEventSink) Received() []domain.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Event(nil), f.events...)
}

func newTestServiceWithEvents(t *testing.T, ctrl *gomock.Controller) (*Service, *mocks.Mockrepository, *fakeEventSink) {
	t.Helper()
	repo := mocks.NewMockrepository(ctrl)
	sink := &fakeEventSink{}
	publisher, err := domain.NewPublisher(sink)
	require.NoError(t, err)
	svc := NewService(repo, ServiceExtraConfig{
		UnsubscribeKey: []byte("test-secret-32-bytes-padded-xx!!"),
		EventPublisher: publisher,
	})
	return svc, repo, sink
}

func TestDeleteProfile_GDPRDeleteAccountCalled(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").
		Return(&models.Account{AccountId: "cust_1", Email: "owner@test.com"}, nil)
	repo.EXPECT().DeleteAccount(gomock.Any(), "cust_1").Return(nil).Times(1)

	err := svc.DeleteProfile(context.Background(), "cust_1", "owner@test.com")
	require.NoError(t, err)
}

func TestDeleteProfile_UnknownAccountErasesNothing(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(nil, db.ErrNotFound)
	repo.EXPECT().DeleteAccount(gomock.Any(), gomock.Any()).Times(0)

	err := svc.DeleteProfile(context.Background(), "cust_1", "owner@test.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestUpdateProfile_OnlyTranslatesSuppliedFields(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	var captured *models.UpdateAccountRequest
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, req *models.UpdateAccountRequest) (*models.Account, error) {
			captured = req
			return &models.Account{AccountId: "cust_1"}, nil
		})

	firstName := "Jane"
	_, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		FirstName: &firstName,
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.FirstName)
	assert.Equal(t, "Jane", *captured.FirstName)
	assert.Nil(t, captured.Phone)
	assert.Nil(t, captured.LastName)
	assert.Nil(t, captured.AvatarUrl)
}

func TestCreateAccount_MissingFields_MapsToInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		accountID string
		req       models.CreateAccountRequest
	}{
		{name: "missing account_id", req: models.CreateAccountRequest{Email: "a@b.co", FirstName: "A", LastName: "B"}},
		{name: "missing email", accountID: "cust_1", req: models.CreateAccountRequest{FirstName: "A", LastName: "B"}},
		{name: "missing first_name", accountID: "cust_1", req: models.CreateAccountRequest{Email: "a@b.co", LastName: "B"}},
		{name: "missing last_name", accountID: "cust_1", req: models.CreateAccountRequest{Email: "a@b.co", FirstName: "A"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			svc, _ := newTestService(t, ctrl)

			_, err := svc.CreateAccount(context.Background(), tc.accountID, &tc.req)
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}

func TestCreateAccount_NilEventPublisher_DoesNotPanic(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().CreateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1", Email: "a@b.co", FirstName: "A", LastName: "B"}, nil)

	created, err := svc.CreateAccount(context.Background(), "cust_1", &models.CreateAccountRequest{
		Email: "a@b.co", FirstName: "A", LastName: "B",
	})
	require.NoError(t, err)
	assert.Equal(t, "cust_1", created.AccountId)
}

func TestCreateAccount_PublishesAccountRegisteredEvent(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)

	repo.EXPECT().CreateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1", Email: "a@b.co", FirstName: "A", LastName: "B"}, nil)

	_, err := svc.CreateAccount(context.Background(), "cust_1", &models.CreateAccountRequest{
		Email: "a@b.co", FirstName: "A", LastName: "B",
	})
	require.NoError(t, err)

	events := sink.Received()
	require.Len(t, events, 1)
	assert.Equal(t, "account.registered", events[0].Type)
	assert.Equal(t, "cust_1", events[0].EntityID)
}

func TestCreateAccount_EventPublishFails_AccountCreationStillSucceeds(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)
	sink.sendErr = errors.New("events api unreachable")

	repo.EXPECT().CreateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1", Email: "a@b.co", FirstName: "A", LastName: "B"}, nil)

	created, err := svc.CreateAccount(context.Background(), "cust_1", &models.CreateAccountRequest{
		Email: "a@b.co", FirstName: "A", LastName: "B",
	})
	require.NoError(t, err)
	assert.Equal(t, "cust_1", created.AccountId)
}

func TestUpdateProfile_EventPublishFails_UpdateStillSucceeds(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)
	sink.sendErr = errors.New("events api unreachable")

	firstName := "Jane"
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1"}, nil)

	updated, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		FirstName: &firstName,
	})
	require.NoError(t, err)
	assert.Equal(t, "cust_1", updated.AccountId)
}

func TestDeleteProfile_EventPublishFails_DeleteStillSucceeds(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)
	sink.sendErr = errors.New("events api unreachable")

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").
		Return(&models.Account{AccountId: "cust_1", Email: "owner@test.com"}, nil)
	repo.EXPECT().DeleteAccount(gomock.Any(), "cust_1").Return(nil)

	err := svc.DeleteProfile(context.Background(), "cust_1", "owner@test.com")
	require.NoError(t, err)
}

func TestUpdateProfile_PriorProfileFetchFails_SkipsPhoneChangedEvent(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)

	newPhone := "+15553334444"
	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").Return(nil, errors.New("transient db error"))
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1", Phone: &newPhone}, nil)

	_, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		Phone: &newPhone,
	})
	require.NoError(t, err)

	events := sink.Received()
	require.Len(t, events, 1)
	assert.Equal(t, "account.profile_updated", events[0].Type)
}

func TestDeleteProfile_NilEventPublisher_DoesNotPanic(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").
		Return(&models.Account{AccountId: "cust_1", Email: "owner@test.com"}, nil)
	repo.EXPECT().DeleteAccount(gomock.Any(), "cust_1").Return(nil)

	err := svc.DeleteProfile(context.Background(), "cust_1", "owner@test.com")
	require.NoError(t, err)
}

func TestUpdateProfile_FirstNameAndPhoneChanged_PublishesBothEvents(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)

	oldPhone := "+15551112222"
	newPhone := "+15553334444"

	repo.EXPECT().GetAccount(gomock.Any(), "cust_1").
		Return(&models.Account{AccountId: "cust_1", Phone: &oldPhone}, nil)
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1", Phone: &newPhone}, nil)

	firstName := "Jane"
	_, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		FirstName: &firstName,
		Phone:     &newPhone,
	})
	require.NoError(t, err)

	events := sink.Received()
	require.Len(t, events, 2)

	types := []string{events[0].Type, events[1].Type}
	assert.Contains(t, types, "account.profile_updated")
	assert.Contains(t, types, "account.phone_changed")
}

func TestUpdateProfile_NoPhoneChange_DoesNotPublishPhoneEvent(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo, sink := newTestServiceWithEvents(t, ctrl)

	firstName := "Jane"
	repo.EXPECT().
		UpdateAccount(gomock.Any(), "cust_1", gomock.Any()).
		Return(&models.Account{AccountId: "cust_1"}, nil)

	_, err := svc.UpdateProfile(context.Background(), "cust_1", &models.UpdateAccountRequest{
		FirstName: &firstName,
	})
	require.NoError(t, err)

	events := sink.Received()
	require.Len(t, events, 1)
	assert.Equal(t, "account.profile_updated", events[0].Type)
}
