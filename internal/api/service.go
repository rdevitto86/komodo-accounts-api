package api

import (
	"encoding/json"
	"net/http"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"komodo-customer-api/internal/cache"
	"komodo-customer-api/internal/db"
	"komodo-customer-api/internal/models"

	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

type ServiceExtraConfig struct {
	S3Client       *awss3.Client
	ExportBucket   string
	UnsubscribeKey []byte
}

type Service struct {
	repo             *db.Repo
	profileCache     *cache.TTLCache[string, *models.User]
	credentialsCache *cache.TTLCache[string, *models.CredentialsResponse]
	s3Client         *awss3.Client
	exportBucket     string
	unsubscribeKey   []byte
}

func NewService(repo *db.Repo, cfg ServiceExtraConfig) *Service {
	return &Service{
		repo:             repo,
		profileCache:     cache.New[string, *models.User](5 * time.Minute),
		credentialsCache: cache.New[string, *models.CredentialsResponse](5 * time.Minute),
		s3Client:         cfg.S3Client,
		exportBucket:     cfg.ExportBucket,
		unsubscribeKey:   cfg.UnsubscribeKey,
	}
}

func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}
