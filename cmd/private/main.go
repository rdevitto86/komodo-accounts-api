package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	awsddbsvc "github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"komodo-customer-api/internal/api"
	"komodo-customer-api/internal/db"

	sdkapi "github.com/rdevitto86/komodo-forge-sdk-go/api"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/handlers/health"
	mw "github.com/rdevitto86/komodo-forge-sdk-go/api/middleware"
	srv "github.com/rdevitto86/komodo-forge-sdk-go/api/server"
	sdkaws "github.com/rdevitto86/komodo-forge-sdk-go/aws"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	awsSM "github.com/rdevitto86/komodo-forge-sdk-go/aws/secretsmanager"
	sdklog "github.com/rdevitto86/komodo-forge-sdk-go/logging"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

const (
	DYNAMODB_TABLE             = "DYNAMODB_TABLE"
	CUSTOMER_API_CLIENT_ID     = "CUSTOMER_API_CLIENT_ID"
	CUSTOMER_API_CLIENT_SECRET = "CUSTOMER_API_CLIENT_SECRET"
	UNSUBSCRIBE_TOKEN_SECRET   = "UNSUBSCRIBE_TOKEN_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_AUDIENCE,
	jwt.JWT_ISSUER,
	CUSTOMER_API_CLIENT_ID,
	CUSTOMER_API_CLIENT_SECRET,
	DYNAMODB_TABLE,
	UNSUBSCRIBE_TOKEN_SECRET,
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

	logger.Info("customer-api internal: bootstrap complete")
	return jwtClient, ddb
}

func main() {
	ctx := context.Background()
	jwtClient, ddb := bootstrap(ctx)

	awsCfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(os.Getenv(sdkaws.AWS_REGION)))
	if err != nil {
		logger.Fatal("failed to load aws config", err)
	}
	rawDDBOpts := []func(*awsddbsvc.Options){}
	if ep := os.Getenv(sdkaws.AWS_ENDPOINT); ep != "" {
		rawDDBOpts = append(rawDDBOpts, func(o *awsddbsvc.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	rawDDB := awsddbsvc.NewFromConfig(awsCfg, rawDDBOpts...)

	repo := db.New(ddb, rawDDB, os.Getenv(DYNAMODB_TABLE))
	svc := api.NewService(repo, api.ServiceExtraConfig{
		UnsubscribeKey: []byte(os.Getenv(UNSUBSCRIBE_TOKEN_SECRET)),
	})

	internalMW := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.AuthMiddleware(jwtClient),
		mw.ScopeMiddleware,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, os.Getenv(DYNAMODB_TABLE)),
	}))

	mux.Handle("GET /v1/users/{id}", mw.Chain(http.HandlerFunc(svc.GetProfileHandler), internalMW...))
	mux.Handle("GET /v1/users/{id}/addresses", mw.Chain(http.HandlerFunc(svc.GetAddressesHandler), internalMW...))
	mux.Handle("GET /v1/users/{id}/preferences", mw.Chain(http.HandlerFunc(svc.GetPreferencesHandler), internalMW...))
	mux.Handle("GET /v1/users/{id}/payments", mw.Chain(http.HandlerFunc(svc.GetPaymentsHandler), internalMW...))
	mux.Handle("GET /v1/users/credentials", mw.Chain(http.HandlerFunc(svc.GetCredentialsHandler), internalMW...))
	mux.Handle("PUT /v1/users/{id}/credentials", mw.Chain(http.HandlerFunc(svc.UpdateCredentialsHandler), internalMW...))

	mux.Handle("GET /v1/users/{id}/passkeys", mw.Chain(http.HandlerFunc(svc.GetPasskeysHandler), internalMW...))
	mux.Handle("POST /v1/users/{id}/passkeys", mw.Chain(http.HandlerFunc(svc.AddPasskeyHandler), internalMW...))
	mux.Handle("PATCH /v1/users/{id}/passkeys/{credential_id}", mw.Chain(http.HandlerFunc(svc.UpdatePasskeyHandler), internalMW...))
	mux.Handle("DELETE /v1/users/{id}/passkeys/{credential_id}", mw.Chain(http.HandlerFunc(svc.DeletePasskeyHandler), internalMW...))

	mux.Handle("GET /v1/customers/{id}/settings", mw.Chain(http.HandlerFunc(svc.GetSettingsHandler), internalMW...))
	mux.Handle("PUT /v1/customers/{id}/settings", mw.Chain(http.HandlerFunc(svc.UpdateSettingsHandler), internalMW...))
	mux.Handle("PUT /v1/customers/{id}/settings/tags", mw.Chain(http.HandlerFunc(svc.UpdateSettingsTagsHandler), internalMW...))

	mux.Handle("POST /internal/v1/customers/{id}/unsubscribe-token", mw.Chain(http.HandlerFunc(svc.MintUnsubscribeTokenHandler), internalMW...))

	server := &http.Server{
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	srv.Run(server, os.Getenv(sdkapi.PORT_PRIVATE), 30*time.Second)
}
