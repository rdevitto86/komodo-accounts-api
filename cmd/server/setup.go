package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"golang.org/x/time/rate"

	"komodo-accounts-api/internal/api"

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
	ACCOUNTS_API_CLIENT_ID     = "ACCOUNTS_API_CLIENT_ID"
	ACCOUNTS_API_CLIENT_SECRET = "ACCOUNTS_API_CLIENT_SECRET"
	S3_EXPORT_BUCKET           = "S3_EXPORT_BUCKET"
	S3_AVATAR_BUCKET           = "S3_AVATAR_BUCKET"
	UNSUBSCRIBE_TOKEN_SECRET   = "UNSUBSCRIBE_TOKEN_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_AUDIENCE,
	jwt.JWT_ISSUER,
	ACCOUNTS_API_CLIENT_ID,
	ACCOUNTS_API_CLIENT_SECRET,
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

// Helper that checks if the application is running in health check mode
func healthCheckMode(args []string) (code int, isHealthCheck bool) {
	if len(args) <= 1 || args[1] != "-healthcheck" {
		return 0, false
	}
	if code := api.HealthProbe(os.Getenv(sdkapi.PORT)); code != 0 {
		return code, true
	}
	return api.HealthProbe(os.Getenv(sdkapi.PORT_PRIVATE)), true
}

// Helper that loads all required dependencies for the application
func bootstrap(ctx context.Context) (
	*jwt.Client,
	*awsddb.Client,
	*sdks3.Client,
) {
	// Initialize runtime logger
	if err := logger.Init(logger.Config{
		Level:  os.Getenv(sdklog.LOG_LEVEL),
		Format: logger.FormatJSON,
		Redact: logger.RedactStrict,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Initialize AWS Secrets Manager client
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

	// Load secrets from AWS Secrets Manager
	secrets, err := sm.GetSecrets(ctx, secretKeys)
	if err != nil {
		logger.Fatal("failed to fetch secrets", err)
	}
	for k, v := range secrets {
		if err := os.Setenv(k, v); err != nil {
			logger.Fatal("failed to set env var from secret", err)
		}
	}

	// Initialize JWT client
	jwtClient, err := jwt.New(ctx, jwt.Config{
		PublicKeyPEM: os.Getenv(jwt.JWT_PUBLIC_KEY),
		Issuer:       os.Getenv(jwt.JWT_ISSUER),
		Audience:     os.Getenv(jwt.JWT_AUDIENCE),
	})
	if err != nil {
		logger.Fatal("failed to initialize jwt verifier", err)
	}

	// Initialize DynamoDB client
	ddb, err := awsddb.New(ctx, awsddb.Config{
		Region:   os.Getenv(sdkaws.AWS_REGION),
		Endpoint: os.Getenv(sdkaws.AWS_ENDPOINT),
	})
	if err != nil {
		logger.Fatal("failed to initialize dynamodb", err)
	}

	// Initialize S3 client
	s3Client, err := sdks3.New(ctx, sdks3.S3Config{
		Region:   os.Getenv(sdkaws.AWS_REGION),
		Endpoint: os.Getenv(sdkaws.AWS_ENDPOINT),
	})
	if err != nil {
		logger.Fatal("failed to initialize s3 client", err)
	}

	logger.Info("accounts-api: bootstrap complete")
	return jwtClient, ddb, s3Client
}

// Helper that creates a rate limiter
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

type publicRoute struct {
	method  string
	path    string
	handler http.HandlerFunc
	chain   []func(http.Handler) http.Handler
}

// Helper that generates versioned paths from a base path and version list
func versionedPaths(path string, versions ...string) []string {
	paths := make([]string, len(versions))
	for i, v := range versions {
		paths[i] = "/" + v + path
	}
	return paths
}

// Helper function that creates versioned routes from a base path and version list
func versioned(
	method, path string,
	handler http.HandlerFunc,
	chain []func(http.Handler) http.Handler,
	versions ...string,
) []publicRoute {
	paths := versionedPaths(path, versions...)
	routes := make([]publicRoute, len(paths))
	for i, p := range paths {
		routes[i] = publicRoute{method, p, handler, chain}
	}
	return routes
}

// Helper that creates public routes with rule validation and middleware chains
func publicRuleValidatedRoutes(svc *api.Service, publicRead, publicWrite []func(http.Handler) http.Handler) []publicRoute {
	var routes []publicRoute
	routes = append(routes, versioned(http.MethodGet, "/me/profile", svc.GetProfileHandler, publicRead, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/me/profile", svc.CreateAccountHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/me/profile", svc.UpdateProfileHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodDelete, "/me/profile", svc.DeleteProfileHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/me/profile/restore", svc.RestoreProfileHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/me/profile/export", svc.ExportProfileHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/me/profile/avatar", svc.AvatarUploadHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/me/addresses", svc.GetAddressesHandler, publicRead, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/me/addresses", svc.AddAddressHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/me/addresses/{id}", svc.UpdateAddressHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodDelete, "/me/addresses/{id}", svc.DeleteAddressHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/me/payments", svc.GetPaymentsHandler, publicRead, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/me/payments", svc.UpsertPaymentHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodDelete, "/me/payments/{id}", svc.DeletePaymentHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/me/preferences", svc.GetPreferencesHandler, publicRead, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/me/preferences", svc.UpdatePreferencesHandler, publicWrite, "v1")...)
	routes = append(routes, versioned(http.MethodDelete, "/me/preferences", svc.DeletePreferencesHandler, publicWrite, "v1")...)
	return routes
}

// Helper that creates the public port mux server for the API with all the necessary middleware
func newPublicMux(svc *api.Service, ddb *awsddb.Client, jwtClient *jwt.Client) *http.ServeMux {
	publicRead := []func(http.Handler) http.Handler{
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

	publicWrite := append(append([]func(http.Handler) http.Handler{}, publicRead...), mw.IdempotencyMiddleware)

	publicUnauth := []func(http.Handler) http.Handler{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware,
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, os.Getenv(DYNAMODB_TABLE)),
	}))

	for _, r := range publicRuleValidatedRoutes(svc, publicRead, publicWrite) {
		mux.Handle(r.method+" "+r.path, mw.Chain(r.handler, r.chain...))
	}

	for _, p := range versionedPaths("/communications/unsubscribe", "v1") {
		mux.Handle("POST "+p, mw.Chain(http.HandlerFunc(svc.UnsubscribeHandler), publicUnauth...))
	}

	for _, p := range versionedPaths("/accounts/exists", "v1") {
		mux.Handle("GET "+p, newExistsRateLimiter()(mw.Chain(http.HandlerFunc(svc.GetAccountExistsHandler), publicUnauth...)))
	}

	return mux
}

// Helper function that creates private routes with rule validation and middleware chains
func privateRoutes(svc *api.Service, internal []func(http.Handler) http.Handler) []publicRoute {
	var routes []publicRoute
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}", svc.GetProfileHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}/addresses", svc.GetAddressesHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}/preferences", svc.GetPreferencesHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}/payments", svc.GetPaymentsHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/credentials", svc.GetCredentialsHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/accounts/{id}/credentials", svc.UpdateCredentialsHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}/passkeys", svc.GetPasskeysHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodPost, "/accounts/{id}/passkeys", svc.AddPasskeyHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodPatch, "/accounts/{id}/passkeys/{credential_id}", svc.UpdatePasskeyHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodDelete, "/accounts/{id}/passkeys/{credential_id}", svc.DeletePasskeyHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodGet, "/accounts/{id}/settings", svc.GetSettingsHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/accounts/{id}/settings", svc.UpdateSettingsHandler, internal, "v1")...)
	routes = append(routes, versioned(http.MethodPut, "/accounts/{id}/settings/tags", svc.UpdateSettingsTagsHandler, internal, "v1")...)
	return routes
}

// Helper function that creates private mux with rule validation and middleware chains
func newPrivateMux(svc *api.Service, ddb *awsddb.Client, jwtClient *jwt.Client) *http.ServeMux {
	internal := []func(http.Handler) http.Handler{
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

	for _, r := range privateRoutes(svc, internal) {
		mux.Handle(r.method+" "+r.path, mw.Chain(r.handler, r.chain...))
	}

	for _, p := range versionedPaths("/accounts/{id}/communications/unsubscribe-token", "v1") {
		mux.Handle("POST /internal"+p, mw.Chain(http.HandlerFunc(svc.MintUnsubscribeTokenHandler), internal...))
	}

	return mux
}
