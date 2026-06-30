package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/models"
)

// ── Unit Tests: ExportProfileHandler ─────────────────────────────────────────

func TestExportProfileHandler_NoS3Client(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "user_abc")
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

	svc.s3Ops = &mockS3Ops{}
	svc.s3Presign = &mockS3Presign{}
	svc.exportBucket = "test-bucket"

	repo.EXPECT().GetUser(gomock.Any(), "user_abc").Return(&models.User{CustomerID: "user_abc", Email: "user@test.com", FirstName: "Test", LastName: "User"}, nil)
	repo.EXPECT().GetSettings(gomock.Any(), "user_abc").Return(&models.AccountSettings{Status: "active"}, nil)
	repo.EXPECT().GetUserPreferences(gomock.Any(), "user_abc").Return(&models.Preferences{Language: "en"}, nil)
	repo.EXPECT().GetUserAddresses(gomock.Any(), "user_abc").Return([]models.Address{}, nil)
	repo.EXPECT().ListPayments(gomock.Any(), "user_abc").Return([]models.PaymentMethod{}, nil)
	repo.EXPECT().ListConsentHistory(gomock.Any(), "user_abc").Return([]models.ConsentLog{}, nil)
	repo.EXPECT().GetUserPasskeys(gomock.Any(), "user_abc").Return([]models.PasskeyCredential{}, nil)

	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestExportProfileHandler_PartialReadFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	svc.s3Ops = &mockS3Ops{}
	svc.s3Presign = &mockS3Presign{}
	svc.exportBucket = "test-bucket"

	repo.EXPECT().GetUser(gomock.Any(), "user_abc").Return(&models.User{CustomerID: "user_abc", Email: "user@test.com", FirstName: "Test", LastName: "User"}, nil)
	repo.EXPECT().GetSettings(gomock.Any(), "user_abc").Return(nil, errors.New("ddb throttled"))

	req := withUserID(makeRequest(t, http.MethodPost, "/v1/me/profile/export", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.ExportProfileHandler(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
