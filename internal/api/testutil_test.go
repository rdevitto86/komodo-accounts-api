package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	"go.uber.org/mock/gomock"

	"komodo-accounts-api/test/mocks"
)

func newTestService(t *testing.T, ctrl *gomock.Controller) (*Service, *mocks.Mockrepository) {
	t.Helper()
	repo := mocks.NewMockrepository(ctrl)
	svc := NewService(repo, ServiceExtraConfig{UnsubscribeKey: []byte("test-secret-32-bytes-padded-xx!!")})
	return svc, repo
}

func makeRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	if body == nil {
		req, err := http.NewRequest(method, path, http.NoBody)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		return req
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}
	req, err := http.NewRequest(method, path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func withAccountID(req *http.Request, id string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeys.USER_ID_KEY, id)
	return req.WithContext(ctx)
}

func withScopes(req *http.Request, scopes []string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeys.SCOPES_KEY, scopes)
	return req.WithContext(ctx)
}

type mockS3 struct {
	getObjectFn     func(ctx context.Context, bucket, key string) ([]byte, error)
	getObjectAsFn   func(ctx context.Context, bucket, key string, out any) error
	putObjectFn     func(ctx context.Context, bucket, key string, data []byte, contentType string) error
	deleteObjectFn  func(ctx context.Context, bucket, key string) error
	listObjectsFn   func(ctx context.Context, bucket, prefix string) ([]string, error)
	deleteObjectsFn func(ctx context.Context, bucket string, keys []string) error
	headBucketFn    func(ctx context.Context, bucket string) error
	presignPutFn    func(ctx context.Context, bucket, key string, ttl time.Duration, contentType string, contentLength int64) (string, error)
	presignGetFn    func(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

func (m *mockS3) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, bucket, key)
	}
	return nil, nil
}

func (m *mockS3) GetObjectAs(ctx context.Context, bucket, key string, out any) error {
	if m.getObjectAsFn != nil {
		return m.getObjectAsFn(ctx, bucket, key, out)
	}
	return nil
}

func (m *mockS3) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	if m.putObjectFn != nil {
		return m.putObjectFn(ctx, bucket, key, data, contentType)
	}
	return nil
}

func (m *mockS3) DeleteObject(ctx context.Context, bucket, key string) error {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, bucket, key)
	}
	return nil
}

func (m *mockS3) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	if m.listObjectsFn != nil {
		return m.listObjectsFn(ctx, bucket, prefix)
	}
	return nil, nil
}

func (m *mockS3) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if m.deleteObjectsFn != nil {
		return m.deleteObjectsFn(ctx, bucket, keys)
	}
	return nil
}

func (m *mockS3) HeadBucket(ctx context.Context, bucket string) error {
	if m.headBucketFn != nil {
		return m.headBucketFn(ctx, bucket)
	}
	return nil
}

func (m *mockS3) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string, contentLength int64) (string, error) {
	if m.presignPutFn != nil {
		return m.presignPutFn(ctx, bucket, key, ttl, contentType, contentLength)
	}
	return "https://example.com/presigned-upload", nil
}

func (m *mockS3) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if m.presignGetFn != nil {
		return m.presignGetFn(ctx, bucket, key, ttl)
	}
	return "https://example.com/presigned-download", nil
}
