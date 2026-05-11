package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type contextKey string

const APIKeyContextKey contextKey = "api_key"

func APIKeyFromContext(ctx context.Context) (store.ApiKey, bool) {
	key, ok := ctx.Value(APIKeyContextKey).(store.ApiKey)
	return key, ok
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			writeAuthError(w, http.StatusUnauthorized, "invalid Authorization header format")
			return
		}

		key, err := s.ValidateKey(r.Context(), token)
		if err != nil {
			slog.Warn("auth failed", "error", err, "prefix", DisplayPrefix(token))
			code := http.StatusUnauthorized
			writeAuthError(w, code, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), APIKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    -32001,
			"message": msg,
		},
	})
}
