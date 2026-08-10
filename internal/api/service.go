package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
	"komodo-accounts-api/internal/ttlcache"

	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	"github.com/rdevitto86/komodo-forge-sdk-go/events/domain"
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
	EventPublisher *domain.Publisher
}

type Service struct {
	repo             repository
	profileCache     *ttlcache.TTLCache[string, *models.Account]
	credentialsCache *ttlcache.TTLCache[string, *models.CredentialsResponse]
	s3               sdks3.API
	exportBucket     string
	unsubscribeKey   []byte
	avatarBucket     string
	presignTTL       time.Duration
	unsubTokenTTL    time.Duration
	cacheTTL         time.Duration
	eventPublisher   *domain.Publisher
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
		profileCache:     ttlcache.New[string, *models.Account](cacheTTL, cacheMaxSize),
		credentialsCache: ttlcache.New[string, *models.CredentialsResponse](cacheTTL, cacheMaxSize),
		exportBucket:     cfg.ExportBucket,
		unsubscribeKey:   cfg.UnsubscribeKey,
		avatarBucket:     cfg.AvatarBucket,
		presignTTL:       presign,
		unsubTokenTTL:    unsubTTL,
		cacheTTL:         cacheTTL,
		eventPublisher:   cfg.EventPublisher,
	}
	if cfg.S3Client != nil {
		svc.s3 = cfg.S3Client
	}
	return svc
}

func (s *Service) EventPublisher() *domain.Publisher {
	return s.eventPublisher
}

// Writes a JSON response and logs encoding errors.
func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}

// Decodes a JSON request body with strict field validation.
func decodeStrict[T any](req *http.Request, dst *T) error {
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("failed to decode request body: %w", err)
	}
	return nil
}

// Determines if an error is a "not found" typed.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, db.ErrNotFound)
}
