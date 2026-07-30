package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/db"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/idempotency"
	sdkserver "github.com/rdevitto86/komodo-forge-sdk-go/api/server"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func main() {
	publicAddr := os.Getenv("PORT")
	privateAddr := os.Getenv("PORT_PRIVATE")

	// checks for health check mode
	if code, isHealthCheck := healthCheckMode(os.Args, publicAddr, privateAddr); isHealthCheck {
		fmt.Fprintf(os.Stdout, "Health check mode enabled. Result: %d\n", code)
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

	restoreWindow := defaultRestoreWindow
	if v, err := time.ParseDuration(os.Getenv("SOFT_DELETE_WINDOW")); err == nil && v > 0 {
		restoreWindow = v
	}
	presignExpiry := defaultPresignExpiry
	if v, err := time.ParseDuration(os.Getenv("S3_PRESIGN_TTL")); err == nil && v > 0 {
		presignExpiry = v
	}
	unsubTokenTTL := defaultUnsubTokenTTL
	if v, err := time.ParseDuration(os.Getenv("UNSUBSCRIBE_TOKEN_TTL")); err == nil && v > 0 {
		unsubTokenTTL = v
	}
	cacheTTL := defaultCacheTTL
	if v, err := time.ParseDuration(os.Getenv("PROFILE_CACHE_TTL")); err == nil && v > 0 {
		cacheTTL = v
	}
	cacheMaxSize := defaultCacheMaxSize
	if v, err := strconv.Atoi(os.Getenv("PROFILE_CACHE_MAX_ENTRIES")); err == nil && v > 0 {
		cacheMaxSize = v
	}

	// init global service
	svc := api.NewService(
		db.New(svr.ddb, ddbTable, db.WithRestoreWindow(restoreWindow)),
		api.ServiceExtraConfig{
			S3Client:       svr.s3Client,
			ExportBucket:   exportBucket,
			UnsubscribeKey: svr.unsubscribeKey,
			AvatarBucket:   avatarBucket,
			PresignTTL:     presignExpiry,
			UnsubTokenTTL:  unsubTokenTTL,
			CacheTTL:       cacheTTL,
			CacheMaxSize:   cacheMaxSize,
		})

	readTimeout := defaultReadTimeout
	if v, err := time.ParseDuration(os.Getenv("SERVER_READ_TIMEOUT")); err == nil && v > 0 {
		readTimeout = v
	}
	writeTimeout := defaultWriteTimeout
	if v, err := time.ParseDuration(os.Getenv("SERVER_WRITE_TIMEOUT")); err == nil && v > 0 {
		writeTimeout = v
	}
	idleTimeout := defaultIdleTimeout
	if v, err := time.ParseDuration(os.Getenv("SERVER_IDLE_TIMEOUT")); err == nil && v > 0 {
		idleTimeout = v
	}
	headerTimeout := defaultHeaderTimeout
	if v, err := time.ParseDuration(os.Getenv("SERVER_READ_HEADER_TIMEOUT")); err == nil && v > 0 {
		headerTimeout = v
	}
	shutdownTimeout := defaultShutdownTimeout
	if v, err := time.ParseDuration(os.Getenv("SERVER_SHUTDOWN_TIMEOUT")); err == nil && v > 0 {
		shutdownTimeout = v
	}

	err := sdkserver.Serve(
		shutdownTimeout,
		sdkserver.Listener{
			Label: "public",
			Addr:  publicAddr,
			Server: &http.Server{
				Handler:           newPublicMux(svc, svr.ddb, svr.verify.user, ddbTable, svr.generalLimiter),
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
				Handler:           newPrivateMux(svc, svr.ddb, svr.verify.service, ddbTable),
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
