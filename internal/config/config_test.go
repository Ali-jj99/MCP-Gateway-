package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("UPSTREAM_URL", "http://localhost:3000")
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("IDLE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("METRICS_ENABLED", "")
	t.Setenv("METRICS_PORT", "")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	t.Setenv("AUDIT_ENABLED", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("MIGRATIONS_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log level info, got %s", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("expected log format json, got %s", cfg.LogFormat)
	}
	if cfg.ReadTimeout != 15*time.Second {
		t.Errorf("expected read timeout 15s, got %s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 30*time.Second {
		t.Errorf("expected write timeout 30s, got %s", cfg.WriteTimeout)
	}
	if cfg.IdleTimeout != 60*time.Second {
		t.Errorf("expected idle timeout 60s, got %s", cfg.IdleTimeout)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("expected shutdown timeout 30s, got %s", cfg.ShutdownTimeout)
	}
	if !cfg.MetricsEnabled {
		t.Error("expected metrics enabled by default")
	}
	if cfg.MetricsPort != 9090 {
		t.Errorf("expected metrics port 9090, got %d", cfg.MetricsPort)
	}
	if !cfg.RateLimitEnabled {
		t.Error("expected rate limit enabled by default")
	}
	if !cfg.AuditEnabled {
		t.Error("expected audit enabled by default")
	}
	if cfg.MigrationsPath != "migrations" {
		t.Errorf("expected migrations path 'migrations', got %s", cfg.MigrationsPath)
	}
}

func TestLoad_MissingUpstreamURL(t *testing.T) {
	t.Setenv("UPSTREAM_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing UPSTREAM_URL")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("UPSTREAM_URL", "http://backend:5000")
	t.Setenv("PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("READ_TIMEOUT", "5s")
	t.Setenv("WRITE_TIMEOUT", "10s")
	t.Setenv("IDLE_TIMEOUT", "30s")
	t.Setenv("SHUTDOWN_TIMEOUT", "15s")
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("METRICS_PORT", "9191")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("AUDIT_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("expected text, got %s", cfg.LogFormat)
	}
	if cfg.ReadTimeout != 5*time.Second {
		t.Errorf("expected 5s, got %s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 10*time.Second {
		t.Errorf("expected 10s, got %s", cfg.WriteTimeout)
	}
	if cfg.MetricsEnabled {
		t.Error("expected metrics disabled")
	}
	if cfg.MetricsPort != 9191 {
		t.Errorf("expected 9191, got %d", cfg.MetricsPort)
	}
	if cfg.RateLimitEnabled {
		t.Error("expected rate limit disabled")
	}
	if cfg.AuditEnabled {
		t.Error("expected audit disabled")
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	t.Setenv("UPSTREAM_URL", "http://localhost:3000")
	t.Setenv("PORT", "99999")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}
