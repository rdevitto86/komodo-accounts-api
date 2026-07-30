package main

import (
	"fmt"
	"strconv"
	"strings"

	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http"
	"github.com/rdevitto86/komodo-forge-sdk-go/security/jwt"
)

type envVar struct{ key, value string }

// Helper that parses and returns a list of missing environment variables.
func missingKeys(vars ...envVar) []string {
	var missing []string
	for _, v := range vars {
		if v.value == "" {
			missing = append(missing, v.key)
		}
	}
	return missing
}

// Helper that loads bootstrap secrets into the local var cache.
func setBootstrapSecrets(vals map[string]string) (
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

	if missing := missingKeys(
		envVar{jwt.JWT_PUBLIC_KEY, jwtPublicKey},
		envVar{jwt.JWT_ISSUER, jwtIssuer},
		envVar{unsubscribeTokenSecret, string(unsubscribeKey)},
	); len(missing) > 0 {
		err = fmt.Errorf("missing required secret-backed environment variables: %s", strings.Join(missing, ", "))
	}
	return jwtPublicKey, jwtIssuer, unsubscribeKey, idempotencyTTLSec, err
}
