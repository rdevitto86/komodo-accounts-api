package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/server"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/idempotency"
	sdkserver "github.com/rdevitto86/komodo-forge-sdk-go/api/server"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func main() {
	publicAddr := os.Getenv("PORT")
	privateAddr := os.Getenv("PORT_PRIVATE")

	// checks for health check mode
	if code, isHealthCheck := healthCheckMode(os.Args, publicAddr, privateAddr); isHealthCheck {
		_, _ = fmt.Fprintf(os.Stdout, "Health check mode enabled. Result: %d\n", code)
		os.Exit(code)
	}

	ddbTable := os.Getenv("DYNAMODB_TABLE")
	exportBucket := os.Getenv("S3_EXPORT_BUCKET")
	avatarBucket := os.Getenv("S3_AVATAR_BUCKET")

	if missing := missingKeys(
		envVar{"PORT", publicAddr},
		envVar{"PORT_PRIVATE", privateAddr},
		envVar{"DYNAMODB_TABLE", ddbTable},
		envVar{"S3_EXPORT_BUCKET", exportBucket},
		envVar{"S3_AVATAR_BUCKET", avatarBucket},
	); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "missing required environment variables: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}

	// load server config and dependencies
	svr := bootstrap()

	// init idempotency store
	idempotency.SetStore(idempotency.NewStore("local", svr.idempotencyTTLSec))
	logger.Info("configured idempotency store",
		logger.Attr("mode", "local"),
		logger.Attr("ttl_sec", svr.idempotencyTTLSec),
	)

	presignExpiry := envDuration("S3_PRESIGN_TTL", defaultPresignExpiry)
	unsubTokenTTL := envDuration("UNSUBSCRIBE_TOKEN_TTL", defaultUnsubTokenTTL)
	cacheTTL := envDuration("PROFILE_CACHE_TTL", defaultCacheTTL)
	cacheMaxSize := envInt("PROFILE_CACHE_MAX_ENTRIES", defaultCacheMaxSize)

	// init global service
	svc := api.NewService(
		db.New(svr.ddb, ddbTable),
		api.ServiceExtraConfig{
			S3Client:       svr.s3Client,
			ExportBucket:   exportBucket,
			UnsubscribeKey: svr.unsubscribeKey,
			AvatarBucket:   avatarBucket,
			PresignTTL:     presignExpiry,
			UnsubTokenTTL:  unsubTokenTTL,
			CacheTTL:       cacheTTL,
			CacheMaxSize:   cacheMaxSize,
			EventPublisher: svr.eventPublisher,
		},
	)

	readTimeout := envDuration("SERVER_READ_TIMEOUT", defaultReadTimeout)
	writeTimeout := envDuration("SERVER_WRITE_TIMEOUT", defaultWriteTimeout)
	idleTimeout := envDuration("SERVER_IDLE_TIMEOUT", defaultIdleTimeout)
	headerTimeout := envDuration("SERVER_READ_HEADER_TIMEOUT", defaultHeaderTimeout)
	shutdownTimeout := envDuration("SERVER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)

	err := sdkserver.Serve(
		shutdownTimeout,
		sdkserver.Listener{
			Label: "public",
			Addr:  publicAddr,
			Server: &http.Server{
				Handler:           server.NewPublicMux(svc, svr.ddb, svr.verify.user, ddbTable, svr.generalLimiter),
				ReadTimeout:       readTimeout,
				WriteTimeout:      writeTimeout,
				IdleTimeout:       idleTimeout,
				ReadHeaderTimeout: headerTimeout,
				MaxHeaderBytes:    1 << 20, // 1 MB
			},
		},
		sdkserver.Listener{
			Label: "private",
			Addr:  privateAddr,
			Server: &http.Server{
				Handler:           server.NewPrivateMux(svc, svr.ddb, svr.verify.service, ddbTable),
				ReadTimeout:       readTimeout,
				WriteTimeout:      writeTimeout,
				IdleTimeout:       idleTimeout,
				ReadHeaderTimeout: headerTimeout,
				MaxHeaderBytes:    1 << 20, // 1 MB
			},
		},
	)
	if err != nil {
		logger.Fatal("server failed to serve", err)
	}
}
