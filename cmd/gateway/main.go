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
	"github.com/Ali-jj99/mcp-gateway/internal/middleware"
	"github.com/Ali-jj99/mcp-gateway/internal/proxy"
	"github.com/Ali-jj99/mcp-gateway/internal/ratelimit"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	proxyHandler, err := proxy.NewHandler(cfg.UpstreamURL)
	if err != nil {
		slog.Error("failed to create proxy", "error", err)
		os.Exit(1)
	}

	if cfg.DatabaseURL != "" {
		db, err := database.Connect(cfg.DatabaseURL)
		if err != nil {
			slog.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		if err := database.Migrate(db, cfg.MigrationsPath); err != nil {
			slog.Error("failed to run migrations", "error", err)
			os.Exit(1)
		}

		adminHandler := admin.NewHandler(db)
		adminHandler.Register(r)

		q := store.New(db)
		authService := auth.NewService(q)

		auditLogger := audit.NewLogger(q, 4096)
		defer auditLogger.Close()

		rateLimiter := ratelimit.NewLimiter(q, ratelimit.DefaultConfig)

		r.With(authService.Middleware, rateLimiter.Middleware, auditLogger.Middleware).Post("/mcp", proxyHandler.ServeHTTP)
	} else {
		slog.Warn("DATABASE_URL not set, auth disabled, admin endpoints disabled")
		r.Post("/mcp", proxyHandler.ServeHTTP)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting gateway", "port", cfg.Port, "upstream", cfg.UpstreamURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
}
