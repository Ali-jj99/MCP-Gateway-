// Package auth handles API key validation and permission checking.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/Ali-jj99/mcp-gateway/internal/models"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func HashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func (s *Service) ValidateKey(key string) (*models.APIKey, error) {
	hash := HashKey(key)

	var apiKey models.APIKey
	err := s.db.QueryRow(`
		SELECT id, name, key_hash, key_prefix, expires_at, active, created_at, updated_at
		FROM api_keys
		WHERE key_hash = $1 AND active = true
		AND (expires_at IS NULL OR expires_at > NOW())
	`, hash).Scan(
		&apiKey.ID, &apiKey.Name, &apiKey.KeyHash, &apiKey.KeyPrefix,
		&apiKey.ExpiresAt, &apiKey.Active, &apiKey.CreatedAt, &apiKey.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("validating key: %w", err)
	}

	if subtle.ConstantTimeCompare([]byte(hash), []byte(apiKey.KeyHash)) != 1 {
		return nil, fmt.Errorf("invalid key")
	}

	return &apiKey, nil
}

func (s *Service) GetPermissions(apiKeyID string) ([]models.Permission, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.role_id, p.resource, p.action
		FROM permissions p
		JOIN roles r ON r.id = p.role_id
		JOIN api_key_roles akr ON akr.role_id = r.id
		WHERE akr.api_key_id = $1
	`, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("querying permissions: %w", err)
	}
	defer rows.Close()

	var perms []models.Permission
	for rows.Next() {
		var p models.Permission
		if err := rows.Scan(&p.ID, &p.RoleID, &p.Resource, &p.Action); err != nil {
			return nil, fmt.Errorf("scanning permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}
