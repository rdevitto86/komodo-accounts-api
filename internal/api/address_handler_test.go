package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

func TestGetAddressesHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		GetAccountAddresses(gomock.Any(), "account_abc").
		Return([]models.Address{{AddressID: "addr_1", Line1: "123 Main St", City: "Springfield", State: "IL", ZipCode: "62701", Country: "US"}}, nil)

	req := withAccountID(makeRequest(t, http.MethodGet, "/v1/me/addresses", nil), "account_abc")
	rr := httptest.NewRecorder()
	svc.GetAddressesHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetAddressesHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodGet, "/v1/me/addresses", nil)
	rr := httptest.NewRecorder()
	svc.GetAddressesHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAddAddressHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		CreateAddress(gomock.Any(), "account_abc", gomock.Any()).
		Return(nil)

	body := map[string]any{"line1": "123 Main St", "city": "Springfield", "state": "IL", "zip_code": "62701", "country": "US"}
	req := withAccountID(makeRequest(t, http.MethodPost, "/v1/me/addresses", body), "account_abc")
	rr := httptest.NewRecorder()
	svc.AddAddressHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestAddAddressHandler_BadJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req, _ := http.NewRequest(http.MethodPost, "/v1/me/addresses", strings.NewReader("not-json"))
	req = withAccountID(req, "account_abc")
	rr := httptest.NewRecorder()
	svc.AddAddressHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAddAddressHandler_NoAuth(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, _ := newTestService(t, ctrl)

	req := makeRequest(t, http.MethodPost, "/v1/me/addresses", map[string]any{"line1": "123 Main St"})
	rr := httptest.NewRecorder()
	svc.AddAddressHandler(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestUpdateAddressHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAddress(gomock.Any(), "account_abc", "addr_1", gomock.Any()).
		Return(nil)

	body := map[string]any{"line1": "456 Elm St", "city": "Springfield", "state": "IL", "zip_code": "62702", "country": "US"}
	req := withAccountID(makeRequest(t, http.MethodPut, "/v1/me/addresses/addr_1", body), "account_abc")
	req.SetPathValue("id", "addr_1")
	rr := httptest.NewRecorder()
	svc.UpdateAddressHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpdateAddressHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		UpdateAddress(gomock.Any(), "account_abc", "addr_missing", gomock.Any()).
		Return(db.ErrNotFound)

	body := map[string]any{"line1": "456 Elm St", "city": "Springfield", "state": "IL", "zip_code": "62702", "country": "US"}
	req := withAccountID(makeRequest(t, http.MethodPut, "/v1/me/addresses/addr_missing", body), "account_abc")
	req.SetPathValue("id", "addr_missing")
	rr := httptest.NewRecorder()
	svc.UpdateAddressHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteAddressHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeleteAddress(gomock.Any(), "account_abc", "addr_1").
		Return(nil)

	req := withAccountID(makeRequest(t, http.MethodDelete, "/v1/me/addresses/addr_1", nil), "account_abc")
	req.SetPathValue("id", "addr_1")
	rr := httptest.NewRecorder()
	svc.DeleteAddressHandler(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestDeleteAddressHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc, repo := newTestService(t, ctrl)

	repo.EXPECT().
		DeleteAddress(gomock.Any(), "account_abc", "addr_missing").
		Return(db.ErrNotFound)

	req := withAccountID(makeRequest(t, http.MethodDelete, "/v1/me/addresses/addr_missing", nil), "account_abc")
	req.SetPathValue("id", "addr_missing")
	rr := httptest.NewRecorder()
	svc.DeleteAddressHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
