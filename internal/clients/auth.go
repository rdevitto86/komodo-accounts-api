package clients

import (
	"fmt"
	"strings"

	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http/client"
)

const (
	DefaultScope = "svc:accounts-api"
	tokenPath    = "/v1/oauth/token"
)

type Config struct {
	AuthAPIBaseURL   string
	EventsAPIBaseURL string
	ClientID         string
	ClientSecret     string
	Scope            string
}

func NewAuthenticatedClient(cfg Config, clientCfg sdkhttp.ClientConfig) (*sdkhttp.Client, error) {
	if cfg.AuthAPIBaseURL == "" {
		return nil, fmt.Errorf("auth api base url is required")
	}

	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client id is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("client secret is required")
	}

	scope := cfg.Scope
	if scope == "" {
		scope = DefaultScope
	}

	clientCfg.Credentials = &sdkhttp.BearerTokenServiceConfig{
		TokenURL:     strings.TrimRight(cfg.AuthAPIBaseURL, "/") + tokenPath,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scope:        scope,
	}

	return sdkhttp.NewClient(clientCfg), nil
}
