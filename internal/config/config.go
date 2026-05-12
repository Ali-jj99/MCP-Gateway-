// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           int
	DatabaseURL    string
	MigrationsPath string
	UpstreamURL    string

	LogLevel  string
	LogFormat string

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	MetricsEnabled bool
	MetricsPort    int

	RateLimitEnabled bool
	AuditEnabled     bool
}

func Load() (*Config, error) {
	upstreamURL := os.Getenv("UPSTREAM_URL")
	if upstreamURL == "" {
		return nil, fmt.Errorf("UPSTREAM_URL is required")
	}

	cfg := &Config{
		Port:             envInt("PORT", 8080),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		MigrationsPath:   envString("MIGRATIONS_PATH", "migrations"),
		UpstreamURL:      upstreamURL,
		LogLevel:         envString("LOG_LEVEL", "info"),
		LogFormat:        envString("LOG_FORMAT", "json"),
		ReadTimeout:      envDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:     envDuration("WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:      envDuration("IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:  envDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		MetricsEnabled:   envBool("METRICS_ENABLED", true),
		MetricsPort:      envInt("METRICS_PORT", 9090),
		RateLimitEnabled: envBool("RATE_LIMIT_ENABLED", true),
		AuditEnabled:     envBool("AUDIT_ENABLED", true),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535")
	}
	if c.MetricsPort < 1 || c.MetricsPort > 65535 {
		return fmt.Errorf("METRICS_PORT must be between 1 and 65535")
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("READ_TIMEOUT must be positive")
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("WRITE_TIMEOUT must be positive")
	}
	return nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
