package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"
)

// ── Unit Tests: GetSettingsHandler ───────────────────────────────────────────

func TestGetSettingsHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active"}, nil)

	req := makeRequest(t, http.MethodGet, "/v1/customers/cust_1/settings", nil)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.GetSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetSettingsHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_missing").
		Return(nil, db.ErrNotFound)

	req := makeRequest(t, http.MethodGet, "/v1/customers/cust_missing/settings", nil)
	req.SetPathValue("id", "cust_missing")
	rr := httptest.NewRecorder()
	svc.GetSettingsHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetSettingsHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/customers//settings", nil)
	rr := httptest.NewRecorder()
	svc.GetSettingsHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// ── Unit Tests: UpdateSettingsHandler ────────────────────────────────────────

func TestUpdateSettingsHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateSettingsPartial(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active"}, nil)

	body := map[string]any{"status": "active"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:customer-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateSettingsHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", nil)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateSettingsHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPut, "/v1/customers//settings", map[string]any{"status": "active"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestUpdateSettingsHandler_PartialUpdate_PreservesFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	changedAt := time.Now().UTC()
	merged := &models.AccountSettings{
		Status:          "suspended",
		StatusReason:    "abuse",
		StatusChangedAt: &changedAt,
		Tags:            []string{"loyalty.vip"},
		EmailVerified:   true,
	}

	repo.EXPECT().
		UpdateSettingsPartial(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(merged, nil)

	body := map[string]any{"status": "suspended", "status_reason": "abuse"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:customer-servicing-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp models.AccountSettings
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, []string{"loyalty.vip"}, resp.Tags)
	assert.True(t, resp.EmailVerified)
	assert.Equal(t, "suspended", resp.Status)
	assert.NotNil(t, resp.StatusChangedAt)
}

func TestUpdateSettingsHandler_StatusChange_StampsStatusChangedAt(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	changedAt := time.Now().UTC()
	repo.EXPECT().
		UpdateSettingsPartial(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "suspended", StatusChangedAt: &changedAt}, nil)

	body := map[string]any{"status": "suspended"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:customer-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp models.AccountSettings
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.NotNil(t, resp.StatusChangedAt)
}

func TestUpdateSettingsHandler_InvalidStatus_Returns400(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"status": "banned"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:customer-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Unit Tests: UpdateSettingsTagsHandler ────────────────────────────────────

func TestUpdateSettingsTagsHandler_ValidNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active"}, nil)
	repo.EXPECT().
		MutateSettingsTags(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).
		Return(nil)
	repo.EXPECT().
		GetSettings(gomock.Any(), "cust_1").
		Return(&models.AccountSettings{Status: "active", Tags: []string{"loyalty.vip"}}, nil)

	body := map[string]any{"add": []string{"loyalty.vip"}, "remove": []string{}}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings/tags", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:loyalty-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsTagsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateSettingsTagsHandler_ForbiddenNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"add": []string{"system.internal"}, "remove": []string{}}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings/tags", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:loyalty-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsTagsHandler(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// ── Unit Tests: UpdateSettingsHandler — scope ACL ────────────────────────────

func TestUpdateSettingsHandler_StatusMutation_WithoutScope_Returns403(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	body := map[string]any{"status": "suspended"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestUpdateSettingsHandler_StatusMutation_WithServiceScope_Returns200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().UpdateSettingsPartial(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().GetSettings(gomock.Any(), "cust_1").Return(&models.AccountSettings{Status: "closed"}, nil)

	body := map[string]any{"status": "closed"}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	req = withScopes(req, []string{"svc:customer-servicing-api"})
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateSettingsHandler_VerifiedFlagOnly_WithoutScope_Returns200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	emailVerified := true
	repo.EXPECT().UpdateSettingsPartial(gomock.Any(), "cust_1", gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().GetSettings(gomock.Any(), "cust_1").Return(&models.AccountSettings{EmailVerified: true}, nil)

	body := map[string]any{"email_verified": emailVerified}
	req := makeRequest(t, http.MethodPut, "/v1/customers/cust_1/settings", body)
	req.SetPathValue("id", "cust_1")
	rr := httptest.NewRecorder()
	svc.UpdateSettingsHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}
