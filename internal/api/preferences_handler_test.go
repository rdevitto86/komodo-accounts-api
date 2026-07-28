package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

func TestGetPreferencesHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountPreferences(gomock.Any(), "account_abc").
		Return(&models.Preferences{Language: ptr("en"), Timezone: ptr("UTC")}, nil)

	req := withAccountId(makeRequest(t, http.MethodGet, "/v1/me/preferences", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.GetPreferencesHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPreferencesHandler_NotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountPreferences(gomock.Any(), "account_missing").
		Return(nil, db.ErrNotFound)

	req := withAccountId(makeRequest(t, http.MethodGet, "/v1/me/preferences", nil), "account_missing")
	rr := httptest.NewRecorder()
	svc.GetPreferencesHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdatePreferencesHandler_MarketingConsentGuard(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{
		"language": "en",
		"marketing": map[string]any{
			"email": "opted_in",
		},
	}
	req := withAccountId(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdatePreferencesHandler_InvalidChannel_Returns400(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"communication": map[string]bool{"unknown_channel": true}}
	req := withAccountId(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdatePreferencesHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAccountPreferences(gomock.Any(), "account_abc", gomock.Any()).
		Return(nil)

	body := map[string]any{"language": "en", "timezone": "UTC"}
	req := withAccountId(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestDeletePreferencesHandler_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeleteAccountPreferences(gomock.Any(), "account_abc").
		Return(nil)

	req := withAccountId(makeRequest(t, http.MethodDelete, "/v1/me/preferences", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.DeletePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}
