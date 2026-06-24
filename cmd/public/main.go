package main

import (
	"context"
	"fmt"
	"komodo-user-api/internal/api"
	"komodo-user-api/internal/db"
	"net/http"
	"os"
	"time"

	sdkapi "github.com/rdevitto86/komodo-forge-sdk-go/api"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/handlers/health"
	mw "github.com/rdevitto86/komodo-forge-sdk-go/api/middleware"
	srv "github.com/rdevitto86/komodo-forge-sdk-go/api/server"
	sdkaws "github.com/rdevitto86/komodo-forge-sdk-go/aws"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	awsSM "github.com/rdevitto86/komodo-forge-sdk-go/aws/secretsmanager"
	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	sdklog "github.com/rdevitto86/komodo-forge-sdk-go/logging"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

const (
	DYNAMODB_TABLE         = "DYNAMODB_TABLE"
	USER_API_CLIENT_ID     = "USER_API_CLIENT_ID"
	USER_API_CLIENT_SECRET = "USER_API_CLIENT_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_AUDIENCE,
	jwt.JWT_ISSUER,
	USER_API_CLIENT_ID,
	USER_API_CLIENT_SECRET,
	DYNAMODB_TABLE,
	sdkhttp.IP_WHITELIST,
	sdkhttp.IP_BLACKLIST,
	sdkhttp.MAX_CONTENT_LENGTH,
	sdkhttp.IDEMPOTENCY_TTL_SEC,
	sdkhttp.RATE_LIMIT_RPS,
	sdkhttp.RATE_LIMIT_BURST,
	sdkhttp.BUCKET_TTL_SECOND,
}

func bootstrap(ctx context.Context) (*jwt.Client, *awsddb.Client) {
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

	logger.Info("user-api public: bootstrap complete")
	return jwtClient, ddb
}

func main() {
	ctx := context.Background()
	jwtClient, ddb := bootstrap(ctx)
	repo := db.New(ddb, os.Getenv(DYNAMODB_TABLE))
	svc := api.NewService(repo)

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.HealthHandler)

	mux.Handle("GET /v1/me/profile", mw.Chain(http.HandlerFunc(svc.GetProfileHandler), publicReadMW...))
	mux.Handle("POST /v1/me/profile", mw.Chain(http.HandlerFunc(svc.CreateUserHandler), publicWriteMW...))
	mux.Handle("PUT /v1/me/profile", mw.Chain(http.HandlerFunc(svc.UpdateProfileHandler), publicWriteMW...))
	mux.Handle("DELETE /v1/me/profile", mw.Chain(http.HandlerFunc(svc.DeleteProfileHandler), publicWriteMW...))

	mux.Handle("GET /v1/me/addresses", mw.Chain(http.HandlerFunc(svc.GetAddressesHandler), publicReadMW...))
	mux.Handle("POST /v1/me/addresses", mw.Chain(http.HandlerFunc(svc.AddAddressHandler), publicWriteMW...))
	mux.Handle("PUT /v1/me/addresses/{id}", mw.Chain(http.HandlerFunc(svc.UpdateAddressHandler), publicWriteMW...))
	mux.Handle("DELETE /v1/me/addresses/{id}", mw.Chain(http.HandlerFunc(svc.DeleteAddressHandler), publicWriteMW...))

	mux.Handle("GET /v1/me/payments", mw.Chain(http.HandlerFunc(svc.GetPaymentsHandler), publicReadMW...))
	mux.Handle("PUT /v1/me/payments", mw.Chain(http.HandlerFunc(svc.UpsertPaymentHandler), publicWriteMW...))
	mux.Handle("DELETE /v1/me/payments/{id}", mw.Chain(http.HandlerFunc(svc.DeletePaymentHandler), publicWriteMW...))

	mux.Handle("GET /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.GetPreferencesHandler), publicReadMW...))
	mux.Handle("PUT /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.UpdatePreferencesHandler), publicWriteMW...))
	mux.Handle("DELETE /v1/me/preferences", mw.Chain(http.HandlerFunc(svc.DeletePreferencesHandler), publicWriteMW...))

	mux.Handle("GET /v1/users/exists", mw.Chain(http.HandlerFunc(svc.GetUserExistsHandler), publicUnauthMW...))

	server := &http.Server{
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	srv.Run(server, os.Getenv(sdkapi.PORT), 30*time.Second)
}
