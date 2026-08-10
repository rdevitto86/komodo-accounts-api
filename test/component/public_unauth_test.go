package component_test

import (
	"net/http"
	"testing"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_Health_IsServedWithoutAuth(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.request(t, http.MethodGet, "/health", nil))

	assertStatus(t, rec, http.StatusOK, "GET /health")
}

func TestComponent_AccountExists_ReachesHandlerWithoutAuth(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccountExistsByEmail(gomock.Any(), gomock.Any()).
		Return(&models.AccountExistsResponse{}, nil).AnyTimes()

	req := h.request(t, http.MethodGet, "/v1/accounts/exists?email=someone@komodo.test", nil)
	rec := serve(h.publicMux, req)

	assertReachedHandler(t, h.publicMux, req, rec)
}

func TestComponent_Unsubscribe_ReachesHandlerWithoutAuth(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().AppendConsentLog(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	body := map[string]any{"token": "not-a-valid-hmac-token"}
	req := h.request(t, http.MethodPost, "/v1/communications/unsubscribe", body)
	rec := serve(h.publicMux, req)

	assertRouteRegistered(t, h.publicMux, req)
	assertErrorCode(t, req, rec, models.Err.InvalidUnsubscribeToken.ID)
}
