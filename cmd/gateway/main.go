// Package main is the entry point for the MCP Gateway server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Ali-jj99/mcp-gateway/internal/admin"
	"github.com/Ali-jj99/mcp-gateway/internal/audit"
	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/config"
	"github.com/Ali-jj99/mcp-gateway/internal/database"
	"github.com/Ali-jj99/mcp-gateway/internal/health"
	"github.com/Ali-jj99/mcp-gateway/internal/metrics"
	"github.com/Ali-jj99/mcp-gateway/internal/middleware"
	"github.com/Ali-jj99/mcp-gateway/internal/proxy"
	"github.com/Ali-jj99/mcp-gateway/internal/ratelimit"
	"github.com/Ali-jj99/mcp-gateway/internal/rbac"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	setupLogger(cfg)

	checker := health.NewChecker(nil)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	if cfg.MetricsEnabled {
		r.Use(metrics.Middleware)
	}

	r.Get("/healthz", checker.Healthz)
	r.Get("/readyz", checker.Readyz)

	proxyHandler, err := proxy.NewHandler(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}

	if cfg.DatabaseURL != "" {
		db, err := database.Connect(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer db.Close()

		if err := database.Migrate(db, cfg.MigrationsPath); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}

		checker.SetDB(db)

		adminHandler := admin.NewHandler(db)
		adminHandler.Register(r)

		q := store.New(db)
		authService := auth.NewService(q)

		var mws []func(http.Handler) http.Handler
		mws = append(mws, authService.Middleware)

		if cfg.RateLimitEnabled {
			rateLimiter := ratelimit.NewLimiter(q, ratelimit.DefaultConfig)
			mws = append(mws, rateLimiter.Middleware)
		}

		rbacService := rbac.NewService(q)
		mws = append(mws, rbacService.Middleware)

		if cfg.AuditEnabled {
			auditLogger := audit.NewLogger(q, 4096)
			defer auditLogger.Close()
			mws = append(mws, auditLogger.Middleware)
		}

		r.With(mws...).Post("/mcp", proxyHandler.ServeHTTP)
	} else {
		slog.Warn("DATABASE_URL not set, auth disabled, admin endpoints disabled")
		r.Post("/mcp", proxyHandler.ServeHTTP)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	errCh := make(chan error, 2)

	go func() {
		slog.Info("starting gateway", "port", cfg.Port, "upstream", cfg.UpstreamURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("gateway server: %w", err)
		}
	}()

	var metricsSrv *http.Server
	if cfg.MetricsEnabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metrics.Handler())
		metricsSrv = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.MetricsPort),
			Handler:      metricsMux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		go func() {
			slog.Info("starting metrics server", "port", cfg.MetricsPort)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("metrics server: %w", err)
			}
		}()
	}

	checker.SetReady(true)
	slog.Info("gateway ready")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("received signal", "signal", sig)
	case err := <-errCh:
		return err
	}

	slog.Info("shutting down gracefully", "timeout", cfg.ShutdownTimeout)
	checker.SetReady(false)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(ctx); err != nil {
			slog.Error("metrics server shutdown error", "error", err)
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("forced shutdown: %w", err)
	}

	slog.Info("shutdown complete")
	return nil
}

func setupLogger(cfg *config.Config) {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}
