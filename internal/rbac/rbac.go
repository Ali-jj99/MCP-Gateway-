package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type PermissionLoader interface {
	GetPermissionsByKeyID(ctx context.Context, apiKeyID uuid.UUID) ([]store.GetPermissionsByKeyIDRow, error)
}

type Service struct {
	loader PermissionLoader
	mu     sync.RWMutex
	cache  map[uuid.UUID][]store.GetPermissionsByKeyIDRow
}

func NewService(loader PermissionLoader) *Service {
	return &Service{
		loader: loader,
		cache:  make(map[uuid.UUID][]store.GetPermissionsByKeyIDRow),
	}
}

// MatchPattern checks whether resource matches a permission pattern.
// Patterns use * as a glob wildcard that matches zero or more characters.
func MatchPattern(pattern, resource string) bool {
	return match(pattern, resource)
}

func match(pattern, value string) bool {
	for len(pattern) > 0 {
		if pattern[0] == '*' {
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(value); i++ {
				if match(pattern, value[i:]) {
					return true
				}
			}
			return false
		}
		if len(value) == 0 || pattern[0] != value[0] {
			return false
		}
		pattern = pattern[1:]
		value = value[1:]
	}
	return len(value) == 0
}

func (s *Service) loadPermissions(ctx context.Context, keyID uuid.UUID) []store.GetPermissionsByKeyIDRow {
	s.mu.RLock()
	if perms, ok := s.cache[keyID]; ok {
		s.mu.RUnlock()
		return perms
	}
	s.mu.RUnlock()

	perms, err := s.loader.GetPermissionsByKeyID(ctx, keyID)
	if err != nil {
		slog.Error("failed to load permissions", "api_key_id", keyID, "error", err)
		return nil
	}

	s.mu.Lock()
	s.cache[keyID] = perms
	s.mu.Unlock()
	return perms
}

// IsAllowed checks whether the given resource and action are permitted for the API key.
// Returns true if no roles are assigned (open by default) or if any permission matches.
func (s *Service) IsAllowed(ctx context.Context, keyID uuid.UUID, resource, action string) bool {
	perms := s.loadPermissions(ctx, keyID)
	if perms == nil {
		return true
	}

	for _, p := range perms {
		if MatchPattern(p.Resource, resource) && MatchPattern(p.Action, action) {
			return true
		}
	}
	return false
}

// InvalidateCache removes cached permissions for a key, forcing a DB reload on next check.
func (s *Service) InvalidateCache(keyID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, keyID)
	s.mu.Unlock()
}

const maxBodyRead = 64 * 1024

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		body := readAndRestore(r)
		resource, action := extractResourceAction(body)

		if resource == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !s.IsAllowed(r.Context(), key.ID, resource, action) {
			slog.Warn("rbac denied",
				"api_key_id", key.ID,
				"resource", resource,
				"action", action,
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32001,
					"message": "permission denied",
				},
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func readAndRestore(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyRead))
	r.Body.Close()
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return string(data)
}

func extractResourceAction(body string) (resource, action string) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(body), &req) != nil {
		return "", ""
	}
	if req.Method == "tools/call" && req.Params.Name != "" {
		return "tool:" + req.Params.Name, "execute"
	}
	return "", ""
}
