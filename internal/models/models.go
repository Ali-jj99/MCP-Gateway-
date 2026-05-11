// Package models defines the core domain types for the MCP Gateway.
package models

import "time"

type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	KeyPrefix string    `json:"key_prefix"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Permission struct {
	ID       string `json:"id"`
	RoleID   string `json:"role_id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type RateLimit struct {
	ID              string `json:"id"`
	APIKeyID        string `json:"api_key_id"`
	RequestsPerMin  int    `json:"requests_per_min"`
	RequestsPerHour int    `json:"requests_per_hour"`
	RequestsPerDay  int    `json:"requests_per_day"`
}

type AuditLog struct {
	ID         string    `json:"id"`
	APIKeyID   string    `json:"api_key_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	StatusCode int       `json:"status_code"`
	Latency    int64     `json:"latency_ms"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}
