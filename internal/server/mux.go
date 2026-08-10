package server

import (
	"net/http"
	"os"
	"strconv"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/api/middleware"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/health"
	mw "github.com/rdevitto86/komodo-forge-sdk-go/api/middleware"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/ratelimit"
	"github.com/rdevitto86/komodo-forge-sdk-go/api/routes"
	"github.com/rdevitto86/komodo-forge-sdk-go/auth"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

const (
	defaultRateLimitRPS   = 1
	defaultRateLimitBurst = 5

	defaultExistsLimitMaxEntries = 50_000
	defaultExistsLimitBucketTTL  = 300
)

// Helper that creates a new public mux server with dependencies.
func NewPublicMux(
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
func NewPrivateMux(svc *api.Service, ddb *awsddb.Client, jwtClient *jwt.Client, ddbTable string) *http.ServeMux {
	internal := []routes.Middleware{
		mw.RequestIDMiddleware,
		mw.TelemetryMiddleware,
		mw.AuthMiddleware(auth.NewJWTVerifier(jwtClient)),
		middleware.JWTScopeObservabilityMiddleware,
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
