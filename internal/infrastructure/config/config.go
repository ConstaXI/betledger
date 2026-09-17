// Package config loads the application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
)

// Config holds the validated application configuration.
type Config struct {
	HTTPPort    string
	DatabaseURL string
	// OIDCIssuerURL is the issuer every accepted token must carry; its
	// discovery document provides the signing keys.
	OIDCIssuerURL string
	// OIDCAudience must appear in the aud claim of every accepted token.
	OIDCAudience string
}

// Load reads the configuration from the environment and fails when a required
// value is missing or invalid.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:      envOrDefault("HTTP_PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		OIDCIssuerURL: os.Getenv("OIDC_ISSUER_URL"),
		OIDCAudience:  envOrDefault("OIDC_AUDIENCE", "betledger-api"),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.HTTPPort == "" {
		return errors.New("HTTP_PORT cannot be empty")
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.OIDCIssuerURL == "" {
		return errors.New("OIDC_ISSUER_URL is required")
	}
	if c.OIDCAudience == "" {
		return errors.New("OIDC_AUDIENCE cannot be empty")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
