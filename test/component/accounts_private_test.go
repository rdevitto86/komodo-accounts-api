package component_test

import (
	"net/http"
	"testing"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_PrivateMux_ServiceCallReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
		Return(&models.Account{AccountId: testAccountID}, nil).AnyTimes()

	rec := serve(h.privateMux, h.serviceCall(t, http.MethodGet, "/v1/accounts/"+testAccountID, nil))

	assertStatus(t, rec, http.StatusOK, "GET /v1/accounts/{id}")
}

func TestComponent_PrivateMux_ListsSubResourcesForServiceCaller(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccountAddresses(gomock.Any(), testAccountID).
		Return([]models.Address{}, nil).AnyTimes()
	h.repo.EXPECT().GetAccountPreferences(gomock.Any(), testAccountID).
		Return(&models.Preferences{}, nil).AnyTimes()
	h.repo.EXPECT().ListPayments(gomock.Any(), testAccountID).
		Return([]models.PaymentMethod{}, nil).AnyTimes()

	for _, path := range []string{
		"/v1/accounts/" + testAccountID + "/addresses",
		"/v1/accounts/" + testAccountID + "/preferences",
		"/v1/accounts/" + testAccountID + "/payments",
	} {
		rec := serve(h.privateMux, h.serviceCall(t, http.MethodGet, path, nil))
		assertStatus(t, rec, http.StatusOK, "GET "+path)
	}
}

func TestComponent_PrivateMux_RejectsUserPlaneToken(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	req := h.request(t, http.MethodGet, "/v1/accounts/"+testAccountID, nil)
	req.Header.Set("Authorization", "Bearer "+h.userToken)

	rec := serve(h.privateMux, req)

	if rec.Code == http.StatusOK {
		t.Fatal("GET /v1/accounts/{id}: a user-plane token reached the private handler and returned 200")
	}
}
