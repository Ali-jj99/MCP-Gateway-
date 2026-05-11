// Package auth handles API key generation, validation, and HTTP middleware.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

const keyPrefix = "mcpgw_"

var (
	ErrMissingKey = errors.New("missing API key")
	ErrInvalidKey = errors.New("invalid API key")
	ErrExpiredKey = errors.New("expired API key")
	ErrRevokedKey = errors.New("revoked API key")
)

type Service struct {
	q store.Querier
}

func NewService(q store.Querier) *Service {
	return &Service{q: q}
}

func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return keyPrefix + hex.EncodeToString(b), nil
}

func HashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func DisplayPrefix(plaintext string) string {
	if len(plaintext) > 14 {
		return plaintext[:14]
	}
	return plaintext
}

func (s *Service) CreateKey(ctx context.Context, name string, expiresAt *time.Time) (string, store.ApiKey, error) {
	plaintext, err := GenerateKey()
	if err != nil {
		return "", store.ApiKey{}, err
	}

	params := store.CreateAPIKeyParams{
		Name:      name,
		KeyHash:   HashKey(plaintext),
		KeyPrefix: DisplayPrefix(plaintext),
	}
	if expiresAt != nil {
		params.ExpiresAt = sql.NullTime{Time: *expiresAt, Valid: true}
	}

	key, err := s.q.CreateAPIKey(ctx, params)
	if err != nil {
		return "", store.ApiKey{}, fmt.Errorf("inserting key: %w", err)
	}

	return plaintext, key, nil
}

func (s *Service) ValidateKey(ctx context.Context, plaintext string) (store.ApiKey, error) {
	hash := HashKey(plaintext)

	key, err := s.q.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ApiKey{}, ErrInvalidKey
		}
		return store.ApiKey{}, fmt.Errorf("looking up key: %w", err)
	}

	if !key.Active {
		return store.ApiKey{}, ErrRevokedKey
	}

	if key.ExpiresAt.Valid && key.ExpiresAt.Time.Before(time.Now()) {
		return store.ApiKey{}, ErrExpiredKey
	}

	return key, nil
}
