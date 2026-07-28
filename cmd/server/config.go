package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

type config struct {
	publicAddr  string
	privateAddr string
	dynamoTable string
}

// Helper that loads server configuration from environment variables.
func loadConfig() (config, error) {
	cfg := config{
		publicAddr:  os.Getenv("PORT"),
		privateAddr: os.Getenv("PORT_PRIVATE"),
		dynamoTable: os.Getenv("DYNAMODB_TABLE"),
	}

	type svrCfg = struct{ key, val string }
	sc := []svrCfg{
		{"PORT", cfg.publicAddr},
		{"PORT_PRIVATE", cfg.privateAddr},
		{"DYNAMODB_TABLE", cfg.dynamoTable},
	}

	var missing []string
	for _, kv := range sc {
		if kv.val == "" {
			missing = append(missing, kv.key)
		}
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// Helper that loads bootstrap secrets into the local var cache.
func loadBootstrapSecrets(vals map[string]string) (
	jwtPublicKey,
	jwtIssuer string,
	unsubscribeKey []byte,
	idempotencyTTLSec int64,
	err error,
) {
	jwtPublicKey = vals[jwt.JWT_PUBLIC_KEY]
	jwtIssuer = vals[jwt.JWT_ISSUER]
	unsubscribeKey = []byte(vals[unsubscribeTokenSecret])

	idempotencyTTLSec = defaultIdempotencyTTLSec
	if v, parseErr := strconv.ParseInt(vals[sdkhttp.IDEMPOTENCY_TTL_SEC], 10, 64); parseErr == nil && v > 0 {
		idempotencyTTLSec = v
	}

	type secretCfg struct {
		key   string
		value string
	}
	sc := []secretCfg{
		{jwt.JWT_PUBLIC_KEY, jwtPublicKey},
		{jwt.JWT_ISSUER, jwtIssuer},
		{unsubscribeTokenSecret, string(unsubscribeKey)},
	}

	var missing []string
	for _, kv := range sc {
		if kv.value == "" {
			missing = append(missing, kv.key)
		}
	}
	if len(missing) > 0 {
		err = fmt.Errorf("missing required secret-backed environment variables: %s", strings.Join(missing, ", "))
	}
	return jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, err
}
