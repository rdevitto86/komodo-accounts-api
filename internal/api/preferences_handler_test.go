package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"
)

// ── Unit Tests: GetPreferencesHandler ────────────────────────────────────────

func TestGetPreferencesHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserPreferences(gomock.Any(), "user_abc").
		Return(&models.Preferences{Language: "en", Timezone: "UTC"}, nil)

	req := withUserID(makeRequest(t, http.MethodGet, "/v1/me/preferences", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.GetPreferencesHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPreferencesHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetUserPreferences(gomock.Any(), "user_missing").
		Return(nil, db.ErrNotFound)

	req := withUserID(makeRequest(t, http.MethodGet, "/v1/me/preferences", nil), "user_missing")
	rr := httptest.NewRecorder()
	svc.GetPreferencesHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── Unit Tests: UpdatePreferencesHandler ─────────────────────────────────────

func TestUpdatePreferencesHandler_MarketingConsentGuard(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{
		"language": "en",
		"marketing": map[string]any{
			"email": "opted_in",
		},
	}
	req := withUserID(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdatePreferencesHandler_InvalidChannel_Returns400(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"communication": map[string]bool{"unknown_channel": true}}
	req := withUserID(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdatePreferencesHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateUserPreferences(gomock.Any(), "user_abc", gomock.Any()).
		Return(nil)

	body := map[string]any{"language": "en", "timezone": "UTC"}
	req := withUserID(makeRequest(t, http.MethodPut, "/v1/me/preferences", body), "user_abc")
	rr := httptest.NewRecorder()
	svc.UpdatePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ── Unit Tests: DeletePreferencesHandler ─────────────────────────────────────

func TestDeletePreferencesHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeleteUserPreferences(gomock.Any(), "user_abc").
		Return(nil)

	req := withUserID(makeRequest(t, http.MethodDelete, "/v1/me/preferences", nil), "user_abc")
	rr := httptest.NewRecorder()
	svc.DeletePreferencesHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}
