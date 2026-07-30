package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"komodo-accounts-api/internal/api"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/health"
	mw "github.com/rdevitto86/komodo-forge-sdk-go/api/middleware"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/ratelimit"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/routes"
	"github.com/rdevitto86/komodo-forge-sdk-go/auth"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	awsSM "github.com/rdevitto86/komodo-forge-sdk-go/aws/secretsmanager"
	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

const (
	defaultBootTimeout     = 30 * time.Second
	defaultReadTimeout     = 5 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultHeaderTimeout   = 2 * time.Second
	defaultShutdownTimeout = 30 * time.Second

	defaultPresignExpiry = 15 * time.Minute
	defaultUnsubTokenTTL = 30 * 24 * time.Hour
	defaultRestoreWindow = 30 * 24 * time.Hour
	defaultCacheTTL      = 60 * time.Second
	defaultCacheMaxSize  = 100_000

	defaultRateLimitRPS   = 1
	defaultRateLimitBurst = 5

	defaultExistsLimitMaxEntries = 50_000
	defaultExistsLimitBucketTTL  = 300

	defaultIdempotencyTTLSec = 300

	defaultGeneralRPS   = 10
	defaultGeneralBurst = 20

	defaultUserAudience    = "komodo-apis:user"
	defaultServiceAudience = "komodo-apis:service"

	unsubscribeTokenSecret = "UNSUBSCRIBE_TOKEN_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_ISSUER,
	sdkhttp.IDEMPOTENCY_TTL_SEC,
	sdkhttp.RATE_LIMIT_RPS,
	sdkhttp.RATE_LIMIT_BURST,
	unsubscribeTokenSecret,
}

type verifiers struct {
	user    *jwt.Client
	service *jwt.Client
}

type bootstrapResult struct {
	verify            verifiers
	ddb               *awsddb.Client
	s3Client          *sdks3.Client
	unsubscribeKey    []byte
	generalLimiter    *ratelimit.Limiter
	idempotencyTTLSec int64
}

func bootstrap() bootstrapResult {
	boot := defaultBootTimeout
	if v, err := time.ParseDuration(os.Getenv("BOOTSTRAP_TIMEOUT")); err == nil && v > 0 {
		boot = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), boot)
	defer cancel()

	// init runtime logger
	if err := logger.Init(logger.Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Format: logger.FormatJSON,
		Redact: logger.RedactStrict,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	awsRegion := os.Getenv("AWS_REGION")
	awsEndpoint := os.Getenv("AWS_ENDPOINT")

	// init secrets manager
	sm, err := awsSM.New(ctx, awsSM.Config{
		Region:     awsRegion,
		Endpoint:   awsEndpoint,
		SecretPath: os.Getenv("AWS_SECRET_PATH"),
		Keys:       secretKeys,
	})
	if err != nil {
		logger.Fatal("failed to initialize secrets manager", err)
	}
	defer sm.Close()

	fetchedSecrets, err := sm.GetSecrets(ctx, secretKeys)
	if err != nil {
		logger.Fatal("failed to fetch secrets", err)
	}

	// store secrets in memory
	jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, err := setBootstrapSecrets(fetchedSecrets)
	if err != nil {
		logger.Fatal("failed to load secret-backed configuration", err)
	}

	var rps float64 = defaultGeneralRPS
	if v, err := strconv.ParseFloat(strings.TrimSpace(fetchedSecrets[sdkhttp.RATE_LIMIT_RPS]), 64); err == nil && v > 0 {
		rps = v
	}
	var burst float64 = defaultGeneralBurst
	if v, err := strconv.ParseFloat(strings.TrimSpace(fetchedSecrets[sdkhttp.RATE_LIMIT_BURST]), 64); err == nil && v > 0 {
		burst = v
	}

	// init rate limiter
	generalLimiter := ratelimit.New(ratelimit.Config{RPS: rps, Burst: burst})

	userAudience := defaultUserAudience
	if v := os.Getenv("JWT_AUDIENCE_USER"); v != "" {
		userAudience = v
	}
	serviceAudience := defaultServiceAudience
	if v := os.Getenv("JWT_AUDIENCE_SERVICE"); v != "" {
		serviceAudience = v
	}

	var (
		verify     verifiers
		ddb        *awsddb.Client
		s3Client   *sdks3.Client
		wg         sync.WaitGroup
		userJWTErr error
		svcJWTErr  error
		ddbErr     error
		s3Err      error
	)

	wg.Add(4)

	// init user-plane jwt service
	go func() {
		defer wg.Done()
		verify.user, userJWTErr = jwt.New(ctx, jwt.Config{
			PublicKeyPEM: jwtPublicKey,
			Issuer:       jwtIssuer,
			Audience:     userAudience,
		})
	}()

	// init service-plane jwt service
	go func() {
		defer wg.Done()
		verify.service, svcJWTErr = jwt.New(ctx, jwt.Config{
			PublicKeyPEM: jwtPublicKey,
			Issuer:       jwtIssuer,
			Audience:     serviceAudience,
		})
	}()

	// init dynamodb client
	go func() {
		defer wg.Done()
		ddb, ddbErr = awsddb.New(ctx, awsddb.Config{
			Region:   awsRegion,
			Endpoint: awsEndpoint,
		})
	}()

	// init s3 client
	go func() {
		defer wg.Done()
		s3Client, s3Err = sdks3.New(ctx, sdks3.S3Config{
			Region:   awsRegion,
			Endpoint: awsEndpoint,
		})
	}()

	wg.Wait()

	switch {
	case userJWTErr != nil:
		logger.Fatal("failed to initialize user-plane jwt verifier", userJWTErr)
	case svcJWTErr != nil:
		logger.Fatal("failed to initialize service-plane jwt verifier", svcJWTErr)
	case ddbErr != nil:
		logger.Fatal("failed to initialize dynamodb", ddbErr)
	case s3Err != nil:
		logger.Fatal("failed to initialize s3 client", s3Err)
	}

	logger.Info("completed bootstrap",
		logger.Attr("user_audience", userAudience),
		logger.Attr("service_audience", serviceAudience),
	)

	return bootstrapResult{
		verify:            verify,
		ddb:               ddb,
		s3Client:          s3Client,
		unsubscribeKey:    unsubscribeKey,
		generalLimiter:    generalLimiter,
		idempotencyTTLSec: idempotencyTTLSec,
	}
}

// Helper that creates a new public mux server with dependencies.
func newPublicMux(
	svc *api.Service, ddb *awsddb.Client, jwtClient *jwt.Client, ddbTable string, generalLimiter *ratelimit.Limiter,
) *http.ServeMux {
	publicRead := []routes.Middleware{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware(generalLimiter),
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
		mw.AuthMiddleware(auth.NewJWTVerifier(jwtClient)),
		mw.CSRFMiddleware,
		mw.NormalizationMiddleware,
		mw.RuleValidationMiddleware,
		mw.SanitizationMiddleware,
	}

	publicWrite := append(append([]routes.Middleware{}, publicRead...), mw.IdempotencyMiddleware)

	publicUnauth := []routes.Middleware{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.RateLimiterMiddleware(generalLimiter),
		mw.CORSMiddleware,
		mw.SecurityHeadersMiddleware,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, ddbTable),
	}))

	v1 := routes.NewRouter(mux, "/v1")

	// /v1/me
	v1.Group("/me", func(me *routes.RouteGroup) {
		// /v1/me/profile
		me.Group("/profile", func(profile *routes.RouteGroup) {
			profile.Use(publicRead...).Get(svc.GetProfileHandler)
		})
		me.Group("/profile", func(profile *routes.RouteGroup) {
			profile.Use(publicWrite...)
			profile.Post(svc.CreateAccountHandler)
			profile.Put(svc.UpdateProfileHandler)
			profile.Delete(svc.DeleteProfileHandler)
		})

		// /v1/me/profile/export
		me.Group("/profile/export").Use(publicWrite...).Post(svc.ExportProfileHandler)

		// /v1/me/profile/email
		me.Group("/profile/email").Use(publicWrite...).Put(svc.UpdateEmailHandler)

		// /v1/me/profile/avatar
		me.Group("/profile/avatar", func(avatar *routes.RouteGroup) {
			avatar.Use(publicWrite...)
			avatar.Post(svc.AvatarUploadHandler)
			avatar.Delete(svc.DeleteAvatarHandler)
		})

		// /v1/me/addresses
		me.Group("/addresses", func(addresses *routes.RouteGroup) {
			addresses.Use(publicRead...).Get(svc.GetAddressesHandler)
		})
		me.Group("/addresses", func(addresses *routes.RouteGroup) {
			addresses.Use(publicWrite...).Post(svc.AddAddressHandler)
		})

		// /v1/me/addresses/{id}
		me.Group("/addresses/{id}", func(address *routes.RouteGroup) {
			address.Use(publicRead...).Get(svc.GetAddressHandler)
		})
		me.Group("/addresses/{id}", func(address *routes.RouteGroup) {
			address.Use(publicWrite...)
			address.Put(svc.UpdateAddressHandler)
			address.Delete(svc.DeleteAddressHandler)
		})

		// /v1/me/payments
		me.Group("/payments", func(payments *routes.RouteGroup) {
			payments.Use(publicRead...).Get(svc.GetPaymentsHandler)
		})
		me.Group("/payments", func(payments *routes.RouteGroup) {
			payments.Use(publicWrite...).Put(svc.UpsertPaymentHandler)
		})

		// /v1/me/payments/{id}
		me.Group("/payments/{id}", func(payment *routes.RouteGroup) {
			payment.Use(publicRead...).Get(svc.GetPaymentHandler)
		})
		me.Group("/payments/{id}").Use(publicWrite...).Delete(svc.DeletePaymentHandler)

		// /v1/me/preferences
		me.Group("/preferences", func(preferences *routes.RouteGroup) {
			preferences.Use(publicRead...).Get(svc.GetPreferencesHandler)
		})
		me.Group("/preferences", func(preferences *routes.RouteGroup) {
			preferences.Use(publicWrite...)
			preferences.Put(svc.UpdatePreferencesHandler)
		})

		// /v1/me/settings
		me.Group("/settings", func(settings *routes.RouteGroup) {
			settings.Use(publicRead...).Get(svc.GetMySettingsHandler)
		})

		// /v1/me/consents
		me.Group("/consents", func(consents *routes.RouteGroup) {
			consents.Use(publicRead...).Get(svc.GetConsentsHandler)
		})
		me.Group("/consents", func(consents *routes.RouteGroup) {
			consents.Use(publicWrite...).Post(svc.RecordConsentHandler)
		})
	})

	// /v1/communications/unsubscribe
	v1.Group("/communications/unsubscribe").Use(publicUnauth...).Post(svc.UnsubscribeHandler)

	maxEntries := defaultExistsLimitMaxEntries
	if v, err := strconv.Atoi(os.Getenv("EXISTS_RATE_LIMIT_MAX_ENTRIES")); err == nil && v > 0 {
		maxEntries = v
	}
	bucketTTL := defaultExistsLimitBucketTTL
	if v, err := strconv.Atoi(os.Getenv("EXISTS_RATE_LIMIT_BUCKET_TTL_SEC")); err == nil && v > 0 {
		bucketTTL = v
	}

	existsLimiter := ratelimit.New(ratelimit.Config{
		RPS:             defaultRateLimitRPS,
		Burst:           defaultRateLimitBurst,
		MaxEntries:      maxEntries,
		BucketTTLSecond: bucketTTL,
	})

	// /v1/accounts/exists
	mux.Handle("GET /v1/accounts/exists", routes.AppendMiddleware(
		svc.GetAccountExistsHandler,
		append([]routes.Middleware{mw.RateLimiterMiddleware(existsLimiter)}, publicUnauth...)...,
	))

	return mux
}

// Helper that creates a new private mux server with dependencies.
func newPrivateMux(svc *api.Service, ddb *awsddb.Client, jwtClient *jwt.Client, ddbTable string) *http.ServeMux {
	internal := []routes.Middleware{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.AuthMiddleware(auth.NewJWTVerifier(jwtClient)),
		mw.ScopeMiddleware,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.HandleFunc("GET /health/ready", health.NewReadyHandler([]health.Checker{
		health.DynamoDBChecker("dynamodb", ddb, ddbTable),
	}))

	v1 := routes.NewRouter(mux, "/v1")
	v1.Use(internal...)

	// /v1/accounts
	v1.Group("/accounts", func(accounts *routes.RouteGroup) {
		// /v1/accounts/credentials
		accounts.Group("/credentials").Get(svc.GetCredentialsHandler)

		// /v1/accounts/{id}
		accounts.Group("/{id}", func(byID *routes.RouteGroup) {
			byID.Get(svc.GetProfileHandler)

			// /v1/accounts/{id}/addresses
			byID.Group("/addresses").Get(svc.GetAddressesHandler)

			// /v1/accounts/{id}/preferences
			byID.Group("/preferences").Get(svc.GetPreferencesHandler)

			// /v1/accounts/{id}/payments
			byID.Group("/payments").Get(svc.GetPaymentsHandler)

			// /v1/accounts/{id}/credentials
			byID.Group("/credentials").Put(svc.UpdateCredentialsHandler)

			// /v1/accounts/{id}/passkeys
			byID.Group("/passkeys", func(passkeys *routes.RouteGroup) {
				passkeys.Get(svc.GetPasskeysHandler)
				passkeys.Post(svc.AddPasskeyHandler)
			})

			// /v1/accounts/{id}/passkeys/{credential_id}
			byID.Group("/passkeys/{credential_id}", func(passkey *routes.RouteGroup) {
				passkey.Patch(svc.UpdatePasskeyHandler)
				passkey.Delete(svc.DeletePasskeyHandler)
			})

			// /v1/accounts/{id}/settings
			byID.Group("/settings", func(settings *routes.RouteGroup) {
				settings.Get(svc.GetSettingsHandler)
				settings.Put(svc.UpdateSettingsHandler)
				settings.Group("/tags").Put(svc.UpdateSettingsTagsHandler)
			})

			// TODO - ensure this will be used
			byID.Group("/communications/unsubscribe-token").Post(svc.MintUnsubscribeTokenHandler)
		})
	})

	return mux
}

// Helper that checks if the health check flag is set and runs the health check.
func healthCheckMode(args []string, publicAddr, privateAddr string) (code int, isHealthCheck bool) {
	if len(args) <= 1 || args[1] != "-healthcheck" {
		return 0, false
	}
	if code := api.HealthProbe(publicAddr); code != 0 {
		return code, true
	}
	return api.HealthProbe(privateAddr), true
}
