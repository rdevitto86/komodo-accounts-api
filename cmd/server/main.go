package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	awsddbsvc "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"komodo-customer-api/internal/api"
	"komodo-customer-api/internal/db"

	sdkapi "github.com/rdevitto86/komodo-forge-sdk-go/api"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/handlers/health"
	mw "github.com/rdevitto86/komodo-forge-sdk-go/api/middleware"
	httpReq "github.com/rdevitto86/komodo-forge-sdk-go/api/request"
	sdkaws "github.com/rdevitto86/komodo-forge-sdk-go/aws"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	awsSM "github.com/rdevitto86/komodo-forge-sdk-go/aws/secretsmanager"
	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	sdklog "github.com/rdevitto86/komodo-forge-sdk-go/logging"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

const (
	DYNAMODB_TABLE             = "DYNAMODB_TABLE"
	CUSTOMER_API_CLIENT_ID     = "CUSTOMER_API_CLIENT_ID"
	CUSTOMER_API_CLIENT_SECRET = "CUSTOMER_API_CLIENT_SECRET"
	S3_EXPORT_BUCKET           = "S3_EXPORT_BUCKET"
	S3_AVATAR_BUCKET           = "S3_AVATAR_BUCKET"
	UNSUBSCRIBE_TOKEN_SECRET   = "UNSUBSCRIBE_TOKEN_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_AUDIENCE,
	jwt.JWT_ISSUER,
	CUSTOMER_API_CLIENT_ID,
	CUSTOMER_API_CLIENT_SECRET,
	DYNAMODB_TABLE,
	sdkhttp.IP_WHITELIST,
	sdkhttp.IP_BLACKLIST,
	sdkhttp.MAX_CONTENT_LENGTH,
	sdkhttp.IDEMPOTENCY_TTL_SEC,
	sdkhttp.RATE_LIMIT_RPS,
	sdkhttp.RATE_LIMIT_BURST,
	sdkhttp.BUCKET_TTL_SECOND,
	S3_EXPORT_BUCKET,
	S3_AVATAR_BUCKET,
	UNSUBSCRIBE_TOKEN_SECRET,
}

func bootstrap(ctx context.Context) (*jwt.Client, *awsddb.Client, *awss3.Client) {
	if err := logger.Init(logger.Config{
		Level:  os.Getenv(sdklog.LOG_LEVEL),
		Format: logger.FormatJSON,
		Redact: logger.RedactStrict,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	sm, err := awsSM.New(ctx, awsSM.Config{
		Region:     os.Getenv(sdkaws.AWS_REGION),
		Endpoint:   os.Getenv(sdkaws.AWS_ENDPOINT),
		SecretPath: os.Getenv(sdkaws.AWS_SECRET_PATH),
		Keys:       secretKeys,
	})
	if err != nil {
		logger.Fatal("failed to initialize secrets manager", err)
	}
	defer sm.Close()

	secrets, err := sm.GetSecrets(ctx, secretKeys)
	if err != nil {
		logger.Fatal("failed to fetch secrets", err)
	}
	for k, v := range secrets {
		os.Setenv(k, v)
	}

	jwtClient, err := jwt.New(ctx, jwt.Config{
		PublicKeyPEM: os.Getenv(jwt.JWT_PUBLIC_KEY),
		Issuer:       os.Getenv(jwt.JWT_ISSUER),
		Audience:     os.Getenv(jwt.JWT_AUDIENCE),
	})
	if err != nil {
		logger.Fatal("failed to initialize jwt verifier", err)
	}

	ddb, err := awsddb.New(ctx, awsddb.Config{
		Region:   os.Getenv(sdkaws.AWS_REGION),
		Endpoint: os.Getenv(sdkaws.AWS_ENDPOINT),
	})
	if err != nil {
		logger.Fatal("failed to initialize dynamodb", err)
	}

	s3Cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(os.Getenv(sdkaws.AWS_REGION)))
	if err != nil {
		logger.Fatal("failed to load aws config for s3", err)
	}
	s3Client := awss3.NewFromConfig(s3Cfg)

	logger.Info("customer-api: bootstrap complete")
	return jwtClient, ddb, s3Client
}

func newExistsRateLimiter() func(http.Handler) http.Handler {
	var limiters sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(wtr http.ResponseWriter, req *http.Request) {
			key := httpReq.GetClientKey(req)
			v, _ := limiters.LoadOrStore(key, rate.NewLimiter(rate.Limit(1), 5))
			if !v.(*rate.Limiter).Allow() {
				wtr.Header().Set("Retry-After", "1")
				http.Error(wtr, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(wtr, req)
		})
	}
}

func main() {
	ctx := context.Background()
	jwtClient, ddb, s3Raw := bootstrap(ctx)

	awsCfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(os.Getenv(sdkaws.AWS_REGION)))
	if err != nil {
		logger.Fatal("failed to load aws config", err)
	}
	rawDDBOpts := []func(*awsddbsvc.Options){}
	if ep := os.Getenv(sdkaws.AWS_ENDPOINT); ep != "" {
		rawDDBOpts = append(rawDDBOpts, func(o *awsddbsvc.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	rawDDB := awsddbsvc.NewFromConfig(awsCfg, rawDDBOpts...)

	avatarS3, err := sdks3.New(ctx, sdks3.S3Config{
		Region:   os.Getenv(sdkaws.AWS_REGION),
		Endpoint: os.Getenv(sdkaws.AWS_ENDPOINT),
	})
	if err != nil {
		logger.Fatal("failed to initialize avatar s3 client", err)
	}

	repo := db.New(ddb, rawDDB, os.Getenv(DYNAMODB_TABLE))
	svc := api.NewService(repo, api.ServiceExtraConfig{
		S3Client:            s3Raw,
		ExportBucket:        os.Getenv(S3_EXPORT_BUCKET),
		UnsubscribeKey:      []byte(os.Getenv(UNSUBSCRIBE_TOKEN_SECRET)),
		AvatarPresignClient: avatarS3,
		AvatarBucket:        os.Getenv(S3_AVATAR_BUCKET),
	})

	// --- middleware ---

	publicReadMW := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware,
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
		mw.AuthMiddleware(jwtClient),
		mw.CSRFMiddleware,
		mw.NormalizationMiddleware,
		mw.RuleValidationMiddleware,
		mw.SanitizationMiddleware,
	}

	publicWriteMW := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware,
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
		mw.AuthMiddleware(jwtClient),
		mw.CSRFMiddleware,
		mw.NormalizationMiddleware,
		mw.RuleValidationMiddleware,
		mw.SanitizationMiddleware,
		mw.IdempotencyMiddleware,
	}

	publicUnauthMW := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware,
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
	}

	internalMW := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.AuthMiddleware(jwtClient),
		mw.ScopeMiddleware,
	}

	// --- public endpoints ---

	pubMux := http.NewServeMux()
	pubMux.HandleFunc("GET /health", health.HealthHandler)
	pubMux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, os.Getenv(DYNAMODB_TABLE)),
	}))

	pubMux.Handle("GET /v1/me/profile", mw.Chain(http.HandlerFunc(svc.GetProfileHandler), publicReadMW...))
	pubMux.Handle("POST /v1/me/profile", mw.Chain(http.HandlerFunc(svc.CreateUserHandler), publicWriteMW...))
	pubMux.Handle("PUT /v1/me/profile", mw.Chain(http.HandlerFunc(svc.UpdateProfileHandler), publicWriteMW...))
	pubMux.Handle("DELETE /v1/me/profile", mw.Chain(http.HandlerFunc(svc.DeleteProfileHandler), publicWriteMW...))
	pubMux.Handle("POST /v1/me/profile/restore", mw.Chain(http.HandlerFunc(svc.RestoreProfileHandler), publicWriteMW...))
	pubMux.Handle("POST /v1/me/profile/export", mw.Chain(http.HandlerFunc(svc.ExportProfileHandler), publicWriteMW...))
	pubMux.Handle("POST /v1/me/profile/avatar", mw.Chain(http.HandlerFunc(svc.AvatarUploadHandler), publicWriteMW...))

	pubMux.Handle("POST /v1/communications/unsubscribe", mw.Chain(http.HandlerFunc(svc.UnsubscribeHandler), publicUnauthMW...))

	pubMux.Handle("GET /v1/me/addresses", mw.Chain(http.HandlerFunc(svc.GetAddressesHandler), publicReadMW...))
	pubMux.Handle("POST /v1/me/addresses", mw.Chain(http.HandlerFunc(svc.AddAddressHandler), publicWriteMW...))
	pubMux.Handle("PUT /v1/me/addresses/{id}", mw.Chain(http.HandlerFunc(svc.UpdateAddressHandler), publicWriteMW...))
	pubMux.Handle("DELETE /v1/me/addresses/{id}", mw.Chain(http.HandlerFunc(svc.DeleteAddressHandler), publicWriteMW...))

	pubMux.Handle("GET /v1/me/payments", mw.Chain(http.HandlerFunc(svc.GetPaymentsHandler), publicReadMW...))
	pubMux.Handle("PUT /v1/me/payments", mw.Chain(http.HandlerFunc(svc.UpsertPaymentHandler), publicWriteMW...))
	pubMux.Handle("DELETE /v1/me/payments/{id}", mw.Chain(http.HandlerFunc(svc.DeletePaymentHandler), publicWriteMW...))

	pubMux.Handle("GET /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.GetPreferencesHandler), publicReadMW...))
	pubMux.Handle("PUT /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.UpdatePreferencesHandler), publicWriteMW...))
	pubMux.Handle("DELETE /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.DeletePreferencesHandler), publicWriteMW...))

	pubMux.Handle("GET /v1/users/exists", newExistsRateLimiter()(mw.Chain(http.HandlerFunc(svc.GetUserExistsHandler), publicUnauthMW...)))

	// --- private endpoints ---

	privMux := http.NewServeMux()
	privMux.HandleFunc("GET /health", health.HealthHandler)
	privMux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, os.Getenv(DYNAMODB_TABLE)),
	}))

	privMux.Handle("GET /v1/users/{id}", mw.Chain(http.HandlerFunc(svc.GetProfileHandler), internalMW...))
	privMux.Handle("GET /v1/users/{id}/addresses", mw.Chain(http.HandlerFunc(svc.GetAddressesHandler), internalMW...))
	privMux.Handle("GET /v1/users/{id}/preferences", mw.Chain(http.HandlerFunc(svc.GetPreferencesHandler), internalMW...))
	privMux.Handle("GET /v1/users/{id}/payments", mw.Chain(http.HandlerFunc(svc.GetPaymentsHandler), internalMW...))
	privMux.Handle("GET /v1/users/credentials", mw.Chain(http.HandlerFunc(svc.GetCredentialsHandler), internalMW...))
	privMux.Handle("PUT /v1/users/{id}/credentials", mw.Chain(http.HandlerFunc(svc.UpdateCredentialsHandler), internalMW...))

	privMux.Handle("GET /v1/users/{id}/passkeys", mw.Chain(http.HandlerFunc(svc.GetPasskeysHandler), internalMW...))
	privMux.Handle("POST /v1/users/{id}/passkeys", mw.Chain(http.HandlerFunc(svc.AddPasskeyHandler), internalMW...))
	privMux.Handle("PATCH /v1/users/{id}/passkeys/{credential_id}", mw.Chain(http.HandlerFunc(svc.UpdatePasskeyHandler), internalMW...))
	privMux.Handle("DELETE /v1/users/{id}/passkeys/{credential_id}", mw.Chain(http.HandlerFunc(svc.DeletePasskeyHandler), internalMW...))

	privMux.Handle("GET /v1/customers/{id}/settings", mw.Chain(http.HandlerFunc(svc.GetSettingsHandler), internalMW...))
	privMux.Handle("PUT /v1/customers/{id}/settings", mw.Chain(http.HandlerFunc(svc.UpdateSettingsHandler), internalMW...))
	privMux.Handle("PUT /v1/customers/{id}/settings/tags", mw.Chain(http.HandlerFunc(svc.UpdateSettingsTagsHandler), internalMW...))

	privMux.Handle(
		"POST /internal/v1/customers/{id}/communications/unsubscribe-token",
		mw.Chain(http.HandlerFunc(svc.MintUnsubscribeTokenHandler), internalMW...),
	)

	pubAddr := os.Getenv(sdkapi.PORT)
	if pubAddr == "" {
		pubAddr = ":7051"
	}
	privAddr := os.Getenv(sdkapi.PORT_PRIVATE)
	if privAddr == "" {
		privAddr = ":7052"
	}

	pubServer := &http.Server{
		Addr:              pubAddr, // port 7051
		Handler:           pubMux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	privServer := &http.Server{
		Addr:              privAddr, // port 7052
		Handler:           privMux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	errCh := make(chan error, 2)
	go func() { errCh <- pubServer.ListenAndServe() }()
	go func() { errCh <- privServer.ListenAndServe() }()
	logger.Fatal("failed to serve", <-errCh)
}
