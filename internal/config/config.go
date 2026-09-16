// Package config loads the application configuration from the environment.
package config

import (
	"fmt"
	"os"
)

// Config holds the validated application configuration.
type Config struct {
	HTTPPort string
}

// Load reads the configuration from the environment and fails when a required
// value is missing or invalid.
func Load() (Config, error) {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	cfg := Config{HTTPPort: port}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func (c Config) validate() error {
	if c.HTTPPort == "" {
		return fmt.Errorf("HTTP_PORT cannot be empty")
	}
	return nil
}
