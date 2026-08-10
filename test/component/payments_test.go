package component_test

import (
	"net/http"
	"testing"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_Payments_SignedInListReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().ListPayments(gomock.Any(), testAccountID).
		Return([]models.PaymentMethod{}, nil).AnyTimes()

	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/payments", nil))

	assertStatus(t, rec, http.StatusOK, "GET /v1/me/payments")
}

func TestComponent_Payments_GetByIDReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/payments/"+testPaymentID, nil))

	assertStatus(t, rec, http.StatusNotImplemented, "GET /v1/me/payments/{id}")
}

func TestComponent_Payments_DeleteReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().DeletePayment(gomock.Any(), testAccountID, testPaymentID).Return(nil).AnyTimes()

	req := h.signedIn(t, http.MethodDelete, "/v1/me/payments/"+testPaymentID, nil)
	rec := serve(h.publicMux, req)

	assertReachedHandler(t, h.publicMux, req, rec)
}
