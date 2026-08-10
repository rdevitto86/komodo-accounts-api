package component_test

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"komodo-accounts-api/internal/models"

	"github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil"
	"go.uber.org/mock/gomock"
)

func TestComponent_Profile_SignedInReadReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
		Return(&models.Account{AccountId: testAccountID}, nil).AnyTimes()

	rec := serve(h.publicMux, h.signedIn(t, http.MethodGet, "/v1/me/profile", nil))

	assertStatus(t, rec, http.StatusOK, "GET /v1/me/profile")
}

func TestComponent_Profile_UnauthenticatedReadIsRejected(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.request(t, http.MethodGet, "/v1/me/profile", nil))

	if rec.Code == http.StatusOK {
		t.Fatal("GET /v1/me/profile: an unauthenticated read reached the handler and returned 200")
	}
}

func TestComponent_Profile_UpdateEmailReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	body := map[string]any{"new_email": "someone@komodo.test"}
	rec := serve(h.publicMux, h.signedIn(t, http.MethodPut, "/v1/me/profile/email", body))

	assertStatus(t, rec, http.StatusNotImplemented, "PUT /v1/me/profile/email")
}

func TestComponent_Profile_DeleteAvatarReturnsNotImplemented(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	rec := serve(h.publicMux, h.signedIn(t, http.MethodDelete, "/v1/me/profile/avatar", nil))

	assertStatus(t, rec, http.StatusNotImplemented, "DELETE /v1/me/profile/avatar")
}

func TestComponent_Profile_ExportReachesHandler(t *testing.T) {
	testutil.Component(t)

	h := newHarness(t)
	h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
		Return(&models.Account{AccountId: testAccountID}, nil).AnyTimes()

	req := h.signedIn(t, http.MethodPost, "/v1/me/profile/export", nil)
	rec := serve(h.publicMux, req)

	assertReachedHandler(t, h.publicMux, req, rec)
}

func TestComponent_Profile_DeleteWithConfirmedEmailErasesAndReturns204(t *testing.T) {
	testutil.Component(t)

	s3 := &recordingS3{}
	h := newHarnessWithS3(t, s3)
	h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
		Return(&models.Account{AccountId: testAccountID, Email: testAccountEmail}, nil)
	h.repo.EXPECT().DeleteAccount(gomock.Any(), testAccountID).Return(nil)

	body := map[string]any{"confirm_email": testAccountEmail}
	req := h.signedIn(t, http.MethodDelete, "/v1/me/profile", body)
	assertRouteRegistered(t, h.publicMux, req)

	rec := serve(h.publicMux, req)
	assertReachedHandler(t, h.publicMux, req, rec)
	assertStatus(t, rec, http.StatusNoContent, "DELETE /v1/me/profile")

	if rec.Body.Len() != 0 {
		t.Fatalf("DELETE /v1/me/profile: 204 must carry no body, got %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Fatalf("DELETE /v1/me/profile: 204 must not set Content-Type, got %q", ct)
	}
	if got := s3.deletedKeys("test-avatar-bucket"); len(got) != 1 || got[0] != "avatars/"+testAccountID+"/avatar" {
		t.Fatalf("DELETE /v1/me/profile: erasure must delete the avatar object, deleted %v", got)
	}
}

func TestComponent_Profile_DeleteConfirmEmailIsCaseInsensitiveAndTrimmed(t *testing.T) {
	testutil.Component(t)

	for _, confirm := range []string{"  " + testAccountEmail + "  ", "OWNER@KOMODO.TEST"} {
		t.Run(confirm, func(t *testing.T) {
			s3 := &recordingS3{}
			h := newHarnessWithS3(t, s3)
			h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
				Return(&models.Account{AccountId: testAccountID, Email: testAccountEmail}, nil)
			h.repo.EXPECT().DeleteAccount(gomock.Any(), testAccountID).Return(nil)

			body := map[string]any{"confirm_email": confirm}
			rec := serve(h.publicMux, h.signedIn(t, http.MethodDelete, "/v1/me/profile", body))
			assertStatus(t, rec, http.StatusNoContent, "DELETE /v1/me/profile")
		})
	}
}

func TestComponent_Profile_DeleteWithoutConfirmEmailErasesNothing(t *testing.T) {
	testutil.Component(t)

	s3 := &recordingS3{}
	h := newHarnessWithS3(t, s3)
	h.repo.EXPECT().DeleteAccount(gomock.Any(), gomock.Any()).Times(0)

	req := h.signedIn(t, http.MethodDelete, "/v1/me/profile", map[string]any{})
	assertRouteRegistered(t, h.publicMux, req)

	rec := serve(h.publicMux, req)
	assertStatus(t, rec, http.StatusBadRequest, "DELETE /v1/me/profile")
	if got := s3.deletedKeys("test-avatar-bucket"); len(got) != 0 {
		t.Fatalf("DELETE /v1/me/profile: an unconfirmed closure must erase nothing, deleted %v", got)
	}
}

func TestComponent_Profile_DeleteWithMismatchedEmailErasesNothing(t *testing.T) {
	testutil.Component(t)

	s3 := &recordingS3{}
	h := newHarnessWithS3(t, s3)
	h.repo.EXPECT().GetAccount(gomock.Any(), testAccountID).
		Return(&models.Account{AccountId: testAccountID, Email: testAccountEmail}, nil)
	h.repo.EXPECT().DeleteAccount(gomock.Any(), gomock.Any()).Times(0)

	body := map[string]any{"confirm_email": "someone@else.test"}
	req := h.signedIn(t, http.MethodDelete, "/v1/me/profile", body)
	rec := serve(h.publicMux, req)

	assertStatus(t, rec, http.StatusBadRequest, "DELETE /v1/me/profile")
	assertErrorCode(t, req, rec, models.Err.InvalidInput.ID)
	if got := s3.deletedKeys("test-avatar-bucket"); len(got) != 0 {
		t.Fatalf("DELETE /v1/me/profile: a mismatched confirmation must erase nothing, deleted %v", got)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

type recordingS3 struct {
	mu      sync.Mutex
	deleted map[string][]string
}

func (r *recordingS3) record(bucket string, keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleted == nil {
		r.deleted = map[string][]string{}
	}
	r.deleted[bucket] = append(r.deleted[bucket], keys...)
}

func (r *recordingS3) deletedKeys(bucket string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.deleted[bucket]...)
}

func (r *recordingS3) GetObject(context.Context, string, string) ([]byte, error) { return nil, nil }

func (r *recordingS3) GetObjectAs(context.Context, string, string, any) error { return nil }

func (r *recordingS3) GetObjectStream(context.Context, string, string) (io.ReadCloser, error) {
	return nil, nil
}

func (r *recordingS3) PutObject(context.Context, string, string, []byte, string) error { return nil }

func (r *recordingS3) DeleteObject(_ context.Context, bucket, key string) error {
	r.record(bucket, key)
	return nil
}

func (r *recordingS3) ListObjects(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (r *recordingS3) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	r.record(bucket, keys...)
	return nil
}

func (r *recordingS3) HeadBucket(context.Context, string) error { return nil }

func (r *recordingS3) PresignPut(
	context.Context, string, string, time.Duration, string, int64,
) (string, error) {
	return "https://example.test/put", nil
}

func (r *recordingS3) PresignGet(context.Context, string, string, time.Duration) (string, error) {
	return "https://example.test/get", nil
}
