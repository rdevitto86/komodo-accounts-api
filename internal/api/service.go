package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"komodo-customer-api/internal/cache"
	"komodo-customer-api/internal/models"

	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func decodeStrict[T any](req *http.Request, dst *T) error {
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("failed to decode request body: %w", err)
	}
	return nil
}

type s3ClientAPI interface {
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *awss3.ListObjectsV2Input, optFns ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
	DeleteObjects(ctx context.Context, params *awss3.DeleteObjectsInput, optFns ...func(*awss3.Options)) (*awss3.DeleteObjectsOutput, error)
}

type s3PresignAPI interface {
	PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type ServiceExtraConfig struct {
	S3Client            *awss3.Client
	ExportBucket        string
	UnsubscribeKey      []byte
	AvatarPresignClient sdks3.API
	AvatarBucket        string
}

type Service struct {
	repo             repository
	profileCache     *cache.TTLCache[string, *models.User]
	credentialsCache *cache.TTLCache[string, *models.CredentialsResponse]
	s3Ops            s3ClientAPI
	s3Presign        s3PresignAPI
	exportBucket     string
	unsubscribeKey   []byte
	avatarPresign    sdks3.API
	avatarBucket     string
}

func NewService(repo repository, cfg ServiceExtraConfig) *Service {
	svc := &Service{
		repo:             repo,
		profileCache:     cache.New[string, *models.User](5*time.Minute, 100_000),
		credentialsCache: cache.New[string, *models.CredentialsResponse](5*time.Minute, 100_000),
		exportBucket:     cfg.ExportBucket,
		unsubscribeKey:   cfg.UnsubscribeKey,
		avatarBucket:     cfg.AvatarBucket,
	}
	if cfg.S3Client != nil {
		svc.s3Ops = cfg.S3Client
		svc.s3Presign = awss3.NewPresignClient(cfg.S3Client)
	}
	if cfg.AvatarPresignClient != nil {
		svc.avatarPresign = cfg.AvatarPresignClient
	}
	return svc
}

func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}
