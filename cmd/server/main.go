package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"komodo-accounts-api/internal/api"
	"komodo-accounts-api/internal/db"

	sdkapi "github.com/rdevitto86/komodo-forge-sdk-go/api"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func main() {
	// Check if we're in health check mode
	if code, isHealthCheck := healthCheckMode(os.Args); isHealthCheck {
		os.Exit(code)
	}

	jwtClient, ddb, s3Client := bootstrap(context.Background())

	// Initialize Dynamo DB client
	repo := db.New(ddb, os.Getenv(DYNAMODB_TABLE))

	// Initialize internal services
	svc := api.NewService(repo, api.ServiceExtraConfig{
		S3Client:       s3Client,
		ExportBucket:   os.Getenv(S3_EXPORT_BUCKET),
		UnsubscribeKey: []byte(os.Getenv(UNSUBSCRIBE_TOKEN_SECRET)),
		AvatarBucket:   os.Getenv(S3_AVATAR_BUCKET),
	})

	pubMux := newPublicMux(svc, ddb, jwtClient)
	privMux := newPrivateMux(svc, ddb, jwtClient)

	pubAddr := os.Getenv(sdkapi.PORT)
	if pubAddr == "" {
		logger.Fatal(
			"failed to start public server",
			fmt.Errorf("environment variable not set"),
			logger.Attr("env_var", sdkapi.PORT),
		)
	}
	privAddr := os.Getenv(sdkapi.PORT_PRIVATE)
	if privAddr == "" {
		logger.Fatal(
			"failed to start private server",
			fmt.Errorf("environment variable not set"),
			logger.Attr("env_var", sdkapi.PORT_PRIVATE),
		)
	}

	pubServer := &http.Server{
		Addr:              pubAddr,
		Handler:           pubMux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	privServer := &http.Server{
		Addr:              privAddr,
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
