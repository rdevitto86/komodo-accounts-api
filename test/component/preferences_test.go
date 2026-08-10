package component_test

import (
	"net/http"
	"testing"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_Preferences_SignedInReadReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccountPreferences(gomock.Any(), testAccountID).
		Return(&models.Preferences{}, nil).AnyTimes()

	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/preferences", nil))

	assertStatus(t, rec, http.StatusOK, "GET /v1/me/preferences")
}

func TestComponent_Preferences_UpdateReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().UpdateAccountPreferences(gomock.Any(), testAccountID, gomock.Any()).
		Return(nil).AnyTimes()
	h.repo.EXPECT().GetAccountPreferences(gomock.Any(), testAccountID).
		Return(&models.Preferences{}, nil).AnyTimes()

	body := map[string]any{"language": "en", "timezone": "UTC"}
	req := h.signedIn(t, http.MethodPut, "/v1/me/preferences", body)
	rec := serve(h.publicMux, req)

	assertReachedHandler(t, h.publicMux, req, rec)
}
