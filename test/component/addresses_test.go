package component_test

import (
	"net/http"
	"testing"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_Addresses_SignedInListReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccountAddresses(gomock.Any(), testAccountID).
		Return([]models.Address{}, nil).AnyTimes()

	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/addresses", nil))

	assertStatus(t, rec, http.StatusOK, "GET /v1/me/addresses")
}

func TestComponent_Addresses_GetByIDReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/addresses/"+testAddressID, nil))

	assertStatus(t, rec, http.StatusNotImplemented, "GET /v1/me/addresses/{id}")
}

func TestComponent_Addresses_CreateReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().CreateAddress(gomock.Any(), testAccountID, gomock.Any()).Return(nil).AnyTimes()

	body := map[string]any{
		"line1":    "1 Test Street",
		"city":     "Testville",
		"state":    "CA",
		"zip_code": "90001",
		"country":  "US",
	}
	req := h.signedIn(t, http.MethodPost, "/v1/me/addresses", body)
	rec := serve(h.publicMux, req)

	assertReachedHandler(t, h.publicMux, req, rec)
}
