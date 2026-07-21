package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/models"
)

func TestExportProfileHandler_NoS3Client(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestExportProfileHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil)
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestExportProfileHandler_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	svc.s3 = &mockS3{}
	svc.exportBucket = "test-bucket"

	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(&models.Account{AccountID: "account_abc", Email: "user@test.com", FirstName: "Test", LastName: "Account"}, nil)
	repo.EXPECT().GetSettings(gomock.Any(), "account_abc").Return(&models.AccountSettings{Status: "active"}, nil)
	repo.EXPECT().GetAccountPreferences(gomock.Any(), "account_abc").Return(&models.Preferences{Language: "en"}, nil)
	repo.EXPECT().GetAccountAddresses(gomock.Any(), "account_abc").Return([]models.Address{}, nil)
	repo.EXPECT().ListPayments(gomock.Any(), "account_abc").Return([]models.PaymentMethod{}, nil)
	repo.EXPECT().ListConsentHistory(gomock.Any(), "account_abc").Return([]models.ConsentLog{}, nil)
	repo.EXPECT().GetAccountPasskeys(gomock.Any(), "account_abc").Return([]models.PasskeyCredential{}, nil)

	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestExportProfileHandler_PartialReadFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	svc.s3 = &mockS3{}
	svc.exportBucket = "test-bucket"

	repo.EXPECT().GetAccount(gomock.Any(), "account_abc").Return(&models.Account{AccountID: "account_abc", Email: "user@test.com", FirstName: "Test", LastName: "Account"}, nil)
	repo.EXPECT().GetSettings(gomock.Any(), "account_abc").Return(nil, errors.New("ddb throttled"))

	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
