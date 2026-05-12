package auth_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockQuerier struct {
	keys map[string]store.ApiKey
}

func newMockQuerier() *mockQuerier {
	return &mockQuerier{keys: make(map[string]store.ApiKey)}
}

func (m *mockQuerier) CreateAPIKey(_ context.Context, arg store.CreateAPIKeyParams) (store.ApiKey, error) {
	key := store.ApiKey{
		ID:        uuid.New(),
		Name:      arg.Name,
		KeyHash:   arg.KeyHash,
		KeyPrefix: arg.KeyPrefix,
		ExpiresAt: arg.ExpiresAt,
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.keys[arg.KeyHash] = key
	return key, nil
}

func (m *mockQuerier) GetAPIKeyByHash(_ context.Context, keyHash string) (store.ApiKey, error) {
	key, ok := m.keys[keyHash]
	if !ok {
		return store.ApiKey{}, sql.ErrNoRows
	}
	return key, nil
}

func (m *mockQuerier) DeleteAPIKey(_ context.Context, id uuid.UUID) error {
	for h, k := range m.keys {
		if k.ID == id {
			delete(m.keys, h)
			return nil
		}
	}
	return nil
}

func (m *mockQuerier) ListAPIKeys(_ context.Context) ([]store.ListAPIKeysRow, error) {
	var rows []store.ListAPIKeysRow
	for _, k := range m.keys {
		rows = append(rows, store.ListAPIKeysRow{
			ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix,
			ExpiresAt: k.ExpiresAt, Active: k.Active,
			CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
		})
	}
	return rows, nil
}

func (m *mockQuerier) RevokeAPIKey(_ context.Context, id uuid.UUID) error {
	for h, k := range m.keys {
		if k.ID == id {
			k.Active = false
			m.keys[h] = k
			return nil
		}
	}
	return nil
}

func (m *mockQuerier) InsertAuditLog(_ context.Context, _ store.InsertAuditLogParams) error {
	return nil
}

func (m *mockQuerier) ListAuditLogs(_ context.Context, _ store.ListAuditLogsParams) ([]store.ListAuditLogsRow, error) {
	return nil, nil
}

func (m *mockQuerier) GetRateLimitByKeyID(_ context.Context, _ uuid.UUID) (store.GetRateLimitByKeyIDRow, error) {
	return store.GetRateLimitByKeyIDRow{}, sql.ErrNoRows
}

func (m *mockQuerier) UpsertRateLimit(_ context.Context, _ store.UpsertRateLimitParams) (store.UpsertRateLimitRow, error) {
	return store.UpsertRateLimitRow{}, nil
}

func (m *mockQuerier) CreateRole(_ context.Context, _ store.CreateRoleParams) (store.Role, error) {
	return store.Role{}, nil
}
func (m *mockQuerier) GetRoleByName(_ context.Context, _ string) (store.Role, error) {
	return store.Role{}, sql.ErrNoRows
}
func (m *mockQuerier) ListRoles(_ context.Context) ([]store.Role, error) { return nil, nil }
func (m *mockQuerier) DeleteRole(_ context.Context, _ uuid.UUID) error   { return nil }
func (m *mockQuerier) AddPermission(_ context.Context, _ store.AddPermissionParams) (store.Permission, error) {
	return store.Permission{}, nil
}
func (m *mockQuerier) ListPermissionsByRole(_ context.Context, _ uuid.UUID) ([]store.Permission, error) {
	return nil, nil
}
func (m *mockQuerier) DeletePermission(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) AssignRoleToKey(_ context.Context, _ store.AssignRoleToKeyParams) error {
	return nil
}
func (m *mockQuerier) RemoveRoleFromKey(_ context.Context, _ store.RemoveRoleFromKeyParams) error {
	return nil
}
func (m *mockQuerier) ListRolesForKey(_ context.Context, _ uuid.UUID) ([]store.Role, error) {
	return nil, nil
}
func (m *mockQuerier) GetPermissionsByKeyID(_ context.Context, _ uuid.UUID) ([]store.GetPermissionsByKeyIDRow, error) {
	return nil, nil
}
func (m *mockQuerier) CountActiveKeys(_ context.Context) (int64, error)    { return 0, nil }
func (m *mockQuerier) CountRequestsToday(_ context.Context) (int64, error) { return 0, nil }
func (m *mockQuerier) CountErrorsToday(_ context.Context) (int64, error)   { return 0, nil }

func (m *mockQuerier) setExpired(hash string) {
	if k, ok := m.keys[hash]; ok {
		t := time.Now().Add(-1 * time.Hour)
		k.ExpiresAt = sql.NullTime{Time: t, Valid: true}
		m.keys[hash] = k
	}
}

// --- Key generation tests ---

func TestGenerateKey(t *testing.T) {
	key, err := auth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "mcpgw_") {
		t.Fatalf("expected mcpgw_ prefix, got %q", key)
	}
	// mcpgw_ (6) + 64 hex chars = 70
	if len(key) != 70 {
		t.Fatalf("expected length 70, got %d", len(key))
	}
}

func TestGenerateKeyUniqueness(t *testing.T) {
	k1, _ := auth.GenerateKey()
	k2, _ := auth.GenerateKey()
	if k1 == k2 {
		t.Fatal("generated keys should be unique")
	}
}

func TestHashKeyDeterministic(t *testing.T) {
	h1 := auth.HashKey("mcpgw_test123")
	h2 := auth.HashKey("mcpgw_test123")
	if h1 != h2 {
		t.Fatal("hash should be deterministic")
	}
}

func TestHashKeyDifferentInputs(t *testing.T) {
	h1 := auth.HashKey("mcpgw_aaa")
	h2 := auth.HashKey("mcpgw_bbb")
	if h1 == h2 {
		t.Fatal("different inputs should produce different hashes")
	}
}

func TestDisplayPrefix(t *testing.T) {
	p := auth.DisplayPrefix("mcpgw_abcdef1234567890")
	if p != "mcpgw_abcdef12" {
		t.Fatalf("expected 'mcpgw_abcdef12', got %q", p)
	}
}

// --- Validation tests ---

func TestValidateKey_Valid(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, _, err := svc.CreateKey(context.Background(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	key, err := svc.ValidateKey(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("expected valid key, got error: %v", err)
	}
	if key.Name != "test" {
		t.Fatalf("expected name 'test', got %q", key.Name)
	}
}

func TestValidateKey_Invalid(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	_, err := svc.ValidateKey(context.Background(), "mcpgw_doesnotexist")
	if err != auth.ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestValidateKey_Expired(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, _, err := svc.CreateKey(context.Background(), "expiring", nil)
	if err != nil {
		t.Fatal(err)
	}

	mock.setExpired(auth.HashKey(plaintext))

	_, err = svc.ValidateKey(context.Background(), plaintext)
	if err != auth.ErrExpiredKey {
		t.Fatalf("expected ErrExpiredKey, got %v", err)
	}
}

func TestValidateKey_Revoked(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, created, err := svc.CreateKey(context.Background(), "revoked", nil)
	if err != nil {
		t.Fatal(err)
	}

	_ = mock.RevokeAPIKey(context.Background(), created.ID)

	_, err = svc.ValidateKey(context.Background(), plaintext)
	if err != auth.ErrRevokedKey {
		t.Fatalf("expected ErrRevokedKey, got %v", err)
	}
}

func TestCreateKey_WithExpiry(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	exp := time.Now().Add(24 * time.Hour)
	plaintext, key, err := svc.CreateKey(context.Background(), "temp", &exp)
	if err != nil {
		t.Fatal(err)
	}

	if !key.ExpiresAt.Valid {
		t.Fatal("expected expiry to be set")
	}

	got, err := svc.ValidateKey(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("key should be valid: %v", err)
	}
	if got.ID != key.ID {
		t.Fatal("returned key ID mismatch")
	}
}

// --- Middleware tests ---

func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok {
			http.Error(w, "no key in context", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_name": key.Name})
	})
}

func TestMiddleware_ValidKey(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, _, err := svc.CreateKey(context.Background(), "mw-test", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["key_name"] != "mw-test" {
		t.Fatalf("expected key_name 'mw-test', got %q", body["key_name"])
	}
}

func TestMiddleware_MissingHeader(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	assertJSONRPCError(t, w, "missing Authorization header")
}

func TestMiddleware_InvalidFormat(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Basic abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	assertJSONRPCError(t, w, "invalid Authorization header format")
}

func TestMiddleware_InvalidKey(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer mcpgw_boguskey")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_ExpiredKey(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, _, err := svc.CreateKey(context.Background(), "expired-mw", nil)
	if err != nil {
		t.Fatal(err)
	}
	mock.setExpired(auth.HashKey(plaintext))

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	assertJSONRPCError(t, w, "expired API key")
}

func TestMiddleware_RevokedKey(t *testing.T) {
	mock := newMockQuerier()
	svc := auth.NewService(mock)

	plaintext, created, err := svc.CreateKey(context.Background(), "revoked-mw", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = mock.RevokeAPIKey(context.Background(), created.ID)

	handler := svc.Middleware(echoHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	assertJSONRPCError(t, w, "revoked API key")
}

func assertJSONRPCError(t *testing.T, w *httptest.ResponseRecorder, expectedMsg string) {
	t.Helper()
	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("expected error code -32001, got %d", resp.Error.Code)
	}
	if resp.Error.Message != expectedMsg {
		t.Fatalf("expected error %q, got %q", expectedMsg, resp.Error.Message)
	}
}
