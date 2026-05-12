package rbac_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/rbac"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockLoader struct {
	mu    sync.Mutex
	perms map[uuid.UUID][]store.GetPermissionsByKeyIDRow
	calls atomic.Int64
}

func newMockLoader() *mockLoader {
	return &mockLoader{perms: make(map[uuid.UUID][]store.GetPermissionsByKeyIDRow)}
}

func (m *mockLoader) GetPermissionsByKeyID(_ context.Context, apiKeyID uuid.UUID) ([]store.GetPermissionsByKeyIDRow, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	perms, ok := m.perms[apiKeyID]
	if !ok {
		return nil, nil
	}
	cp := make([]store.GetPermissionsByKeyIDRow, len(perms))
	copy(cp, perms)
	return cp, nil
}

func (m *mockLoader) addPerm(keyID uuid.UUID, resource, action string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.perms[keyID] = append(m.perms[keyID], store.GetPermissionsByKeyIDRow{
		Resource: resource,
		Action:   action,
	})
}

// --- Pattern matching tests ---

func TestMatchPattern_ExactMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		resource string
		want     bool
	}{
		{"tool:read_customer", "tool:read_customer", true},
		{"tool:read_customer", "tool:read_transactions", false},
		{"tool:get_weather", "tool:get_weather", true},
		{"tool:get_weather", "tool:get_forecast", false},
		{"", "", true},
		{"a", "b", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.resource, func(t *testing.T) {
			got := rbac.MatchPattern(tt.pattern, tt.resource)
			if got != tt.want {
				t.Fatalf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.resource, got, tt.want)
			}
		})
	}
}

func TestMatchPattern_WildcardSuffix(t *testing.T) {
	tests := []struct {
		pattern  string
		resource string
		want     bool
	}{
		{"tool:read_*", "tool:read_customer", true},
		{"tool:read_*", "tool:read_transactions", true},
		{"tool:read_*", "tool:read_", true},
		{"tool:read_*", "tool:write_customer", false},
		{"tool:read_*", "tool:read", false},
		{"tool:*", "tool:read_customer", true},
		{"tool:*", "tool:write_customer", true},
		{"tool:*", "tool:", true},
		{"tool:*", "tool:anything_at_all", true},
		{"*", "tool:read_customer", true},
		{"*", "", true},
		{"*", "anything", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.resource, func(t *testing.T) {
			got := rbac.MatchPattern(tt.pattern, tt.resource)
			if got != tt.want {
				t.Fatalf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.resource, got, tt.want)
			}
		})
	}
}

func TestMatchPattern_WildcardMiddle(t *testing.T) {
	tests := []struct {
		pattern  string
		resource string
		want     bool
	}{
		{"tool:*_file", "tool:read_file", true},
		{"tool:*_file", "tool:write_file", true},
		{"tool:*_file", "tool:read_customer", false},
		{"tool:*_file", "tool:_file", true},
		{"*:read_customer", "tool:read_customer", true},
		{"*:read_customer", "resource:read_customer", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.resource, func(t *testing.T) {
			got := rbac.MatchPattern(tt.pattern, tt.resource)
			if got != tt.want {
				t.Fatalf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.resource, got, tt.want)
			}
		})
	}
}

func TestMatchPattern_MultipleWildcards(t *testing.T) {
	tests := []struct {
		pattern  string
		resource string
		want     bool
	}{
		{"tool:*_*", "tool:read_customer", true},
		{"tool:*_*", "tool:a_b", true},
		{"tool:*_*", "tool:nounderscorehere", false},
		{"*:*", "tool:anything", true},
		{"*:*", "x:y", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.resource, func(t *testing.T) {
			got := rbac.MatchPattern(tt.pattern, tt.resource)
			if got != tt.want {
				t.Fatalf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.resource, got, tt.want)
			}
		})
	}
}

// --- IsAllowed tests ---

func TestIsAllowed_NoRoles_AllowsByDefault(t *testing.T) {
	loader := newMockLoader()
	svc := rbac.NewService(loader)

	keyID := uuid.New()
	// No permissions set — key has no roles.
	if !svc.IsAllowed(context.Background(), keyID, "tool:anything", "execute") {
		t.Fatal("expected allow when key has no roles")
	}
}

func TestIsAllowed_ExactMatch(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_customer", "execute")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected allow for exact match")
	}
	if svc.IsAllowed(context.Background(), keyID, "tool:write_customer", "execute") {
		t.Fatal("expected deny for non-matching tool")
	}
}

func TestIsAllowed_WildcardMatch(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected allow for tool:read_customer matching tool:read_*")
	}
	if !svc.IsAllowed(context.Background(), keyID, "tool:read_transactions", "execute") {
		t.Fatal("expected allow for tool:read_transactions matching tool:read_*")
	}
	if svc.IsAllowed(context.Background(), keyID, "tool:write_customer", "execute") {
		t.Fatal("expected deny for tool:write_customer against tool:read_*")
	}
}

func TestIsAllowed_FullWildcard(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:*", "execute")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected allow for tool:* matching anything")
	}
	if !svc.IsAllowed(context.Background(), keyID, "tool:write_customer", "execute") {
		t.Fatal("expected allow for tool:* matching anything")
	}
	if !svc.IsAllowed(context.Background(), keyID, "tool:delete_everything", "execute") {
		t.Fatal("expected allow for tool:* matching anything")
	}
}

func TestIsAllowed_ActionWildcard(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_customer", "*")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected allow for action wildcard")
	}
	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "list") {
		t.Fatal("expected allow for action wildcard with different action")
	}
}

func TestIsAllowed_ActionMismatch(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_customer", "list")

	svc := rbac.NewService(loader)

	if svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected deny when action does not match")
	}
}

func TestIsAllowed_MultipleRoles(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")
	loader.addPerm(keyID, "tool:write_*", "execute")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute") {
		t.Fatal("expected allow via first role")
	}
	if !svc.IsAllowed(context.Background(), keyID, "tool:write_customer", "execute") {
		t.Fatal("expected allow via second role")
	}
	if svc.IsAllowed(context.Background(), keyID, "tool:delete_customer", "execute") {
		t.Fatal("expected deny for tool not covered by any role")
	}
}

func TestIsAllowed_PermissionsCached(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:*", "execute")

	svc := rbac.NewService(loader)

	svc.IsAllowed(context.Background(), keyID, "tool:a", "execute")
	svc.IsAllowed(context.Background(), keyID, "tool:b", "execute")
	svc.IsAllowed(context.Background(), keyID, "tool:c", "execute")

	if loader.calls.Load() != 1 {
		t.Fatalf("expected 1 DB call (cached), got %d", loader.calls.Load())
	}
}

func TestIsAllowed_InvalidateCacheForceReload(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:old_tool", "execute")

	svc := rbac.NewService(loader)
	svc.IsAllowed(context.Background(), keyID, "tool:old_tool", "execute")

	if loader.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", loader.calls.Load())
	}

	svc.InvalidateCache(keyID)
	svc.IsAllowed(context.Background(), keyID, "tool:old_tool", "execute")

	if loader.calls.Load() != 2 {
		t.Fatalf("expected 2 calls after invalidation, got %d", loader.calls.Load())
	}
}

func TestIsAllowed_DifferentKeysIndependent(t *testing.T) {
	loader := newMockLoader()
	key1 := uuid.New()
	key2 := uuid.New()
	loader.addPerm(key1, "tool:read_*", "execute")
	loader.addPerm(key2, "tool:write_*", "execute")

	svc := rbac.NewService(loader)

	if !svc.IsAllowed(context.Background(), key1, "tool:read_customer", "execute") {
		t.Fatal("key1 should be allowed read")
	}
	if svc.IsAllowed(context.Background(), key1, "tool:write_customer", "execute") {
		t.Fatal("key1 should not be allowed write")
	}

	if svc.IsAllowed(context.Background(), key2, "tool:read_customer", "execute") {
		t.Fatal("key2 should not be allowed read")
	}
	if !svc.IsAllowed(context.Background(), key2, "tool:write_customer", "execute") {
		t.Fatal("key2 should be allowed write")
	}
}

// --- Middleware tests ---

func withAPIKey(r *http.Request, key store.ApiKey) *http.Request {
	ctx := context.WithValue(r.Context(), auth.APIKeyContextKey, key)
	return r.WithContext(ctx)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	})
}

func toolCallBody(toolName string) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName},
	})
	return string(b)
}

func TestMiddleware_AllowedTool(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:get_weather", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("get_weather")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMiddleware_DeniedTool(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:get_weather", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_everything")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Error.Code != -32001 {
		t.Fatalf("expected error code -32001, got %d", body.Error.Code)
	}
	if body.Error.Message != "permission denied" {
		t.Fatalf("expected 'permission denied', got %q", body.Error.Message)
	}
}

func TestMiddleware_WildcardPermission(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())

	for _, tool := range []string{"read_customer", "read_transactions", "read_orders"} {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody(tool)))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for tool %q, got %d", tool, w.Code)
		}
	}

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("write_customer")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for write_customer, got %d", w.Code)
	}
}

func TestMiddleware_FullWildcard(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:*", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())

	for _, tool := range []string{"read_customer", "write_customer", "delete_everything", "any_tool"} {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody(tool)))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for tool %q with tool:*, got %d", tool, w.Code)
		}
	}
}

func TestMiddleware_NoAPIKey_PassesThrough(t *testing.T) {
	loader := newMockLoader()
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("anything")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without API key, got %d", w.Code)
	}
}

func TestMiddleware_NoRoles_PassesThrough(t *testing.T) {
	loader := newMockLoader()
	svc := rbac.NewService(loader)
	keyID := uuid.New()

	handler := svc.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("anything")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with no roles assigned, got %d", w.Code)
	}
}

func TestMiddleware_NonToolsCall_PassesThrough(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:get_weather", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())

	bodies := []string{
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"resources/read","params":{"uri":"file:///tmp"}}`,
		`{"jsonrpc":"2.0","method":"initialize"}`,
		`{}`,
		`not json`,
	}

	for _, body := range bodies {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for non-tools/call body %q, got %d", body, w.Code)
		}
	}
}

func TestMiddleware_BodyPreservedForDownstream(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:*", "execute")
	svc := rbac.NewService(loader)

	var downstream map[string]any
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&downstream)
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.Middleware(inner)
	body := toolCallBody("read_customer")
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if downstream["method"] != "tools/call" {
		t.Fatalf("downstream handler should read body, got method=%v", downstream["method"])
	}
}

func TestMiddleware_MultiplePermissions(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")
	loader.addPerm(keyID, "tool:get_weather", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())

	allowed := []string{"read_customer", "read_transactions", "get_weather"}
	for _, tool := range allowed {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody(tool)))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for %q, got %d", tool, w.Code)
		}
	}

	denied := []string{"write_customer", "delete_everything"}
	for _, tool := range denied {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody(tool)))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for %q, got %d", tool, w.Code)
		}
	}
}

// --- Concurrent access ---

func TestIsAllowed_ConcurrentAccess(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")
	svc := rbac.NewService(loader)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.IsAllowed(context.Background(), keyID, "tool:read_customer", "execute")
		}()
	}
	wg.Wait()

	if loader.calls.Load() != 1 {
		t.Fatalf("expected 1 DB call under concurrency, got %d", loader.calls.Load())
	}
}

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.addPerm(keyID, "tool:read_*", "execute")
	svc := rbac.NewService(loader)

	handler := svc.Middleware(okHandler())

	var wg sync.WaitGroup
	var allowed atomic.Int64
	var denied atomic.Int64

	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("read_customer")))
			req = withAPIKey(req, store.ApiKey{ID: keyID})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code == 200 {
				allowed.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("write_customer")))
			req = withAPIKey(req, store.ApiKey{ID: keyID})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code == 403 {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	if allowed.Load() != 50 {
		t.Fatalf("expected 50 allowed, got %d", allowed.Load())
	}
	if denied.Load() != 50 {
		t.Fatalf("expected 50 denied, got %d", denied.Load())
	}
}
