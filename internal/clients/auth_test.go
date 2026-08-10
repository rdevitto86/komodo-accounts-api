package clients

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkhttp "github.com/rdevitto86/komodo-forge-sdk-go/http/client"
)

func validConfig() Config {
	return Config{
		AuthAPIBaseURL: "https://auth.internal.komodo.test",
		ClientID:       "accounts-api",
		ClientSecret:   "super-secret",
	}
}

func TestNewAuthenticatedClient_MissingAuthAPIBaseURL_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.AuthAPIBaseURL = ""

	client, err := NewAuthenticatedClient(cfg, sdkhttp.ClientConfig{})
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestNewAuthenticatedClient_MissingClientID_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.ClientID = ""

	client, err := NewAuthenticatedClient(cfg, sdkhttp.ClientConfig{})
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestNewAuthenticatedClient_MissingClientSecret_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.ClientSecret = ""

	client, err := NewAuthenticatedClient(cfg, sdkhttp.ClientConfig{})
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestNewAuthenticatedClient_ValidConfig_ReturnsClient(t *testing.T) {
	client, err := NewAuthenticatedClient(validConfig(), sdkhttp.ClientConfig{})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewAuthenticatedClient_EmptyScope_DefaultsWithoutError(t *testing.T) {
	cfg := validConfig()
	cfg.Scope = ""

	client, err := NewAuthenticatedClient(cfg, sdkhttp.ClientConfig{})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewAuthenticatedClient_ExplicitScope_ReturnsClient(t *testing.T) {
	cfg := validConfig()
	cfg.Scope = "svc:custom-scope"

	client, err := NewAuthenticatedClient(cfg, sdkhttp.ClientConfig{})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestDefaultScope_Value(t *testing.T) {
	assert.Equal(t, "svc:accounts-api", DefaultScope)
}
