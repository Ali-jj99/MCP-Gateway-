// Package main provides a CLI tool to generate API keys for the MCP Gateway.
//
// Usage:
//
//	DATABASE_URL=... go run ./cmd/keygen -name "my-key"
//	DATABASE_URL=... go run ./cmd/keygen -name "temp-key" -expires 24h
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/database"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

func main() {
	name := flag.String("name", "admin", "Name for the API key")
	expires := flag.Duration("expires", 0, "Key expiration duration (e.g. 24h, 720h). 0 means no expiry")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}

	db, err := database.Connect(dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "migrations"
	}
	if err := database.Migrate(db, migrationsPath); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	q := store.New(db)
	svc := auth.NewService(q)

	var expiresAt *time.Time
	if *expires > 0 {
		t := time.Now().Add(*expires)
		expiresAt = &t
	}

	plaintext, key, err := svc.CreateKey(context.Background(), *name, expiresAt)
	if err != nil {
		slog.Error("failed to create key", "error", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Key created successfully:\n")
	fmt.Fprintf(os.Stderr, "  ID:      %s\n", key.ID)
	fmt.Fprintf(os.Stderr, "  Name:    %s\n", key.Name)
	fmt.Fprintf(os.Stderr, "  Prefix:  %s\n", key.KeyPrefix)
	if key.ExpiresAt.Valid {
		fmt.Fprintf(os.Stderr, "  Expires: %s\n", key.ExpiresAt.Time.Format(time.RFC3339))
	} else {
		fmt.Fprintf(os.Stderr, "  Expires: never\n")
	}
	fmt.Fprintf(os.Stderr, "\nStore this key securely — it cannot be retrieved again:\n\n")
	fmt.Println(plaintext)
}
