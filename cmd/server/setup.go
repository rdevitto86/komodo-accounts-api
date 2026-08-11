package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/clients"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/ratelimit"
	awsddb "github.com/rdevitto86/komodo-forge-sdk-go/aws/dynamodb"
	sdks3 "github.com/rdevitto86/komodo-forge-sdk-go/aws/s3"
	awsSM "github.com/rdevitto86/komodo-forge-sdk-go/aws/secretsmanager"
	"github.com/rdevitto86/komodo-forge-sdk-go/events/domain"
	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	sdkhttpclient "github.com/rdevitto86/komodo-forge-sdk-go/http/client"
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
	defaultCacheTTL      = 60 * time.Second
	defaultCacheMaxSize  = 100_000

	defaultIdempotencyTTLSec = 300

	defaultGeneralRPS   = 10
	defaultGeneralBurst = 20

	defaultUserAudience    = "komodo-apis:user"
	defaultServiceAudience = "komodo-apis:service"

	unsubscribeTokenSecret  = "UNSUBSCRIBE_TOKEN_SECRET"
	accountsAPIClientSecret = "ACCOUNTS_API_CLIENT_SECRET"
)

var secretKeys = []string{
	jwt.JWT_PUBLIC_KEY,
	jwt.JWT_ISSUER,
	sdkhttp.IDEMPOTENCY_TTL_SEC,
	sdkhttp.RATE_LIMIT_RPS,
	sdkhttp.RATE_LIMIT_BURST,
	unsubscribeTokenSecret,
	accountsAPIClientSecret,
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
	eventPublisher    *domain.Publisher
}

type envVar struct{ key, value string }

// Preloads all the required dependencies for the server.
func bootstrap() bootstrapResult {
	boot := envDuration("BOOTSTRAP_TIMEOUT", defaultBootTimeout)

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
	jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, clientSecret, err := setBootstrapSecrets(fetchedSecrets)
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

	verify, ddb, s3Client := initClients(ctx, awsRegion, awsEndpoint, jwtPublicKey, jwtIssuer, userAudience, serviceAudience)

	eventPublisher := initOutboundClients(
		os.Getenv("AUTH_API_BASE_URL"),
		os.Getenv("EVENTS_API_BASE_URL"),
		os.Getenv("ACCOUNTS_API_CLIENT_ID"),
		clientSecret,
	)

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
		eventPublisher:    eventPublisher,
	}
}

func initClients(
	ctx context.Context,
	awsRegion, awsEndpoint string,
	jwtPublicKey, jwtIssuer string,
	userAudience, serviceAudience string,
) (verifiers, *awsddb.Client, *sdks3.Client) {
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

	return verify, ddb, s3Client
}

func initOutboundClients(authAPIBaseURL, eventsAPIBaseURL, clientID, clientSecret string) *domain.Publisher {
	if missing := missingKeys(
		envVar{"AUTH_API_BASE_URL", authAPIBaseURL},
		envVar{"EVENTS_API_BASE_URL", eventsAPIBaseURL},
		envVar{"ACCOUNTS_API_CLIENT_ID", clientID},
	); len(missing) > 0 {
		logger.Fatal("missing required outbound client environment variables",
			fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", ")))
	}

	clientCfg := clients.Config{
		AuthAPIBaseURL:   authAPIBaseURL,
		EventsAPIBaseURL: eventsAPIBaseURL,
		ClientID:         clientID,
		ClientSecret:     clientSecret,
	}

	authedClient, err := clients.NewAuthenticatedClient(clientCfg, sdkhttpclient.ClientConfig{})
	if err != nil {
		logger.Fatal("failed to initialize authenticated service client", err)
	}

	eventPublisher, err := clients.NewPublisher(clientCfg, authedClient)
	if err != nil {
		logger.Fatal("failed to initialize domain event publisher", err)
	}

	return eventPublisher
}

// Checks if the health check flag is set and runs the health check.
func healthCheckMode(args []string, publicAddr, privateAddr string) (code int, isHealthCheck bool) {
	if len(args) <= 1 || args[1] != "-healthcheck" {
		return 0, false
	}
	if code := api.HealthProbe(publicAddr); code != 0 {
		return code, true
	}
	return api.HealthProbe(privateAddr), true
}

// Parses and returns a list of missing environment variables.
func missingKeys(vars ...envVar) []string {
	var missing []string
	for _, v := range vars {
		if v.value == "" {
			missing = append(missing, v.key)
		}
	}
	return missing
}

// Loads bootstrap secrets into the local var cache.
func setBootstrapSecrets(vals map[string]string) (
	jwtPublicKey,
	jwtIssuer string,
	unsubscribeKey []byte,
	idempotencyTTLSec int64,
	clientSecret string,
	err error,
) {
	jwtPublicKey = vals[jwt.JWT_PUBLIC_KEY]
	jwtIssuer = vals[jwt.JWT_ISSUER]
	unsubscribeKey = []byte(vals[unsubscribeTokenSecret])
	clientSecret = vals[accountsAPIClientSecret]

	idempotencyTTLSec = defaultIdempotencyTTLSec
	if v, parseErr := strconv.ParseInt(vals[sdkhttp.IDEMPOTENCY_TTL_SEC], 10, 64); parseErr == nil && v > 0 {
		idempotencyTTLSec = v
	}

	if missing := missingKeys(
		envVar{jwt.JWT_PUBLIC_KEY, jwtPublicKey},
		envVar{jwt.JWT_ISSUER, jwtIssuer},
		envVar{unsubscribeTokenSecret, string(unsubscribeKey)},
		envVar{accountsAPIClientSecret, clientSecret},
	); len(missing) > 0 {
		err = fmt.Errorf("missing required secret-backed environment variables: %s", strings.Join(missing, ", "))
	}
	return jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, clientSecret, err
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}
