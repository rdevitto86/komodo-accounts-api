package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	"go.uber.org/mock/gomock"

	"komodo-customer-api/internal/api/mocks"
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

func withUserID(req *http.Request, id string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeys.USER_ID_KEY, id)
	return req.WithContext(ctx)
}

func withScopes(req *http.Request, scopes []string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeys.SCOPES_KEY, scopes)
	return req.WithContext(ctx)
}

// ── Fake: s3ClientAPI ────────────────────────────────────────────────────────

type mockS3Ops struct {
	putFn    func(context.Context, *awss3.PutObjectInput, ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	listFn   func(context.Context, *awss3.ListObjectsV2Input, ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
	deleteFn func(context.Context, *awss3.DeleteObjectsInput, ...func(*awss3.Options)) (*awss3.DeleteObjectsOutput, error)
}

func (m *mockS3Ops) PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	if m.putFn != nil {
		return m.putFn(ctx, params, optFns...)
	}
	return &awss3.PutObjectOutput{}, nil
}

func (m *mockS3Ops) ListObjectsV2(ctx context.Context, params *awss3.ListObjectsV2Input, optFns ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error) {
	if m.listFn != nil {
		return m.listFn(ctx, params, optFns...)
	}
	return &awss3.ListObjectsV2Output{}, nil
}

func (m *mockS3Ops) DeleteObjects(ctx context.Context, params *awss3.DeleteObjectsInput, optFns ...func(*awss3.Options)) (*awss3.DeleteObjectsOutput, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, params, optFns...)
	}
	return &awss3.DeleteObjectsOutput{}, nil
}

// ── Fake: s3PresignAPI ───────────────────────────────────────────────────────

type mockS3Presign struct {
	presignFn func(context.Context, *awss3.GetObjectInput, ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

func (m *mockS3Presign) PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.presignFn != nil {
		return m.presignFn(ctx, params, optFns...)
	}
	return &v4.PresignedHTTPRequest{URL: "https://example.com/presigned-download"}, nil
}
