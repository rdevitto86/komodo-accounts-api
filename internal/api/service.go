package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"komodo-accounts-api/internal/cache"
	"komodo-accounts-api/internal/models"

	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

type ServiceExtraConfig struct {
	S3Client       sdks3.API
	ExportBucket   string
	UnsubscribeKey []byte
	AvatarBucket   string
	PresignTTL     time.Duration
	UnsubTokenTTL  time.Duration
	CacheTTL       time.Duration
	CacheMaxSize   int
}

type Service struct {
	repo             repository
	profileCache     *cache.TTLCache[string, *models.Account]
	credentialsCache *cache.TTLCache[string, *models.CredentialsResponse]
	s3               sdks3.API
	exportBucket     string
	unsubscribeKey   []byte
	avatarBucket     string
	presignTTL       time.Duration
	unsubTokenTTL    time.Duration
	cacheTTL         time.Duration
}

func NewService(repo repository, cfg ServiceExtraConfig) *Service {
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 60 * time.Second
	}
	cacheMaxSize := cfg.CacheMaxSize
	if cacheMaxSize <= 0 {
		cacheMaxSize = 100_000
	}
	presign := cfg.PresignTTL
	if presign <= 0 {
		presign = 15 * time.Minute
	}
	unsubTTL := cfg.UnsubTokenTTL
	if unsubTTL <= 0 {
		unsubTTL = 30 * 24 * time.Hour
	}

	svc := &Service{
		repo:             repo,
		profileCache:     cache.New[string, *models.Account](cacheTTL, cacheMaxSize),
		credentialsCache: cache.New[string, *models.CredentialsResponse](cacheTTL, cacheMaxSize),
		exportBucket:     cfg.ExportBucket,
		unsubscribeKey:   cfg.UnsubscribeKey,
		avatarBucket:     cfg.AvatarBucket,
		presignTTL:       presign,
		unsubTokenTTL:    unsubTTL,
		cacheTTL:         cacheTTL,
	}
	if cfg.S3Client != nil {
		svc.s3 = cfg.S3Client
	}
	return svc
}

func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}

func decodeStrict[T any](req *http.Request, dst *T) error {
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("failed to decode request body: %w", err)
	}
	return nil
}
