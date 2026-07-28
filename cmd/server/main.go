package main

import (
	"fmt"
	"net/http"
	"os"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/db"

	"github.com/rdevitto86/komodo-forge-sdk-go/api/idempotency"
	sdkserver "github.com/rdevitto86/komodo-forge-sdk-go/api/server"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if code, isHealthCheck := healthCheckMode(os.Args, &cfg); isHealthCheck {
		os.Exit(code)
	}

	boot := bootstrap()

	idempotency.SetStore(idempotency.NewStore("local", boot.idempotencyTTLSec))
	logger.Info("configured idempotency store",
		logger.Attr("mode", "local"),
		logger.Attr("ttl_sec", boot.idempotencyTTLSec),
	)

	repo := db.New(boot.ddb, cfg.dynamoTable, db.WithRestoreWindow(boot.tuning.restoreWindow))

	svc := api.NewService(repo, api.ServiceExtraConfig{
		S3Client:       boot.s3Client,
		ExportBucket:   boot.tuning.exportBucket,
		UnsubscribeKey: boot.unsubscribeKey,
		AvatarBucket:   boot.tuning.avatarBucket,
		PresignTTL:     boot.tuning.presignExpiry,
		UnsubTokenTTL:  boot.tuning.unsubTokenTTL,
		CacheTTL:       boot.tuning.cacheTTL,
		CacheMaxSize:   boot.tuning.cacheMaxSize,
	})

	err = sdkserver.Serve(
		boot.timeouts.shutdown,
		sdkserver.Listener{
			Label: "public",
			Addr:  cfg.publicAddr,
			Server: &http.Server{
				Handler:           newPublicMux(svc, boot.ddb, boot.verify.user, &cfg, boot.generalLimiter),
				ReadTimeout:       boot.timeouts.read,
				WriteTimeout:      boot.timeouts.write,
				IdleTimeout:       boot.timeouts.idle,
				ReadHeaderTimeout: boot.timeouts.header,
				MaxHeaderBytes:    1 << 20,
			},
		},
		sdkserver.Listener{
			Label: "private",
			Addr:  cfg.privateAddr,
			Server: &http.Server{
				Handler:           newPrivateMux(svc, boot.ddb, boot.verify.service, &cfg),
				ReadTimeout:       boot.timeouts.read,
				WriteTimeout:      boot.timeouts.write,
				IdleTimeout:       boot.timeouts.idle,
				ReadHeaderTimeout: boot.timeouts.header,
				MaxHeaderBytes:    1 << 20,
			},
		},
	)
	if err != nil {
		logger.Fatal("server failed to serve", err)
	}
}
