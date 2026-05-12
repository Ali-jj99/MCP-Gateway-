package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockLoader struct {
	mu       sync.Mutex
	policies []store.Policy
}

func (m *mockLoader) ListEnabledPolicies(_ context.Context) ([]store.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.Policy, len(m.policies))
	copy(cp, m.policies)
	return cp, nil
}

func (m *mockLoader) addPolicy(name, policyType string, config any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, _ := json.Marshal(config)
	m.policies = append(m.policies, store.Policy{
		ID:         uuid.New(),
		Name:       name,
		PolicyType: policyType,
		Enabled:    true,
		Config:     cfg,
	})
}

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

func toolCallBodyWithArgs(toolName string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": args},
	})
	return string(b)
}

// --- Time-Based Policy Tests ---

func TestTimeBased_BlockedDuringWindow(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("night-block", "time_based", TimeBasedConfig{
		BlockStartHour: 22,
		BlockEndHour:   6,
		Timezone:       "UTC",
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 23, 30, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("get_data")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 during blocked hours, got %d", w.Code)
	}
}

func TestTimeBased_AllowedOutsideWindow(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("night-block", "time_based", TimeBasedConfig{
		BlockStartHour: 22,
		BlockEndHour:   6,
		Timezone:       "UTC",
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("get_data")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 outside blocked hours, got %d", w.Code)
	}
}

func TestTimeBased_SameDayWindow(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("lunch-block", "time_based", TimeBasedConfig{
		BlockStartHour: 12,
		BlockEndHour:   13,
		Timezone:       "UTC",
	})

	engine := NewEngine(loader)

	tests := []struct {
		name string
		hour int
		want int
	}{
		{"before window", 11, http.StatusOK},
		{"start of window", 12, http.StatusForbidden},
		{"end of window", 13, http.StatusOK},
		{"after window", 14, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine.nowFunc = func() time.Time {
				return time.Date(2025, 1, 15, tt.hour, 0, 0, 0, time.UTC)
			}
			handler := engine.Middleware(okHandler())
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("hour %d: expected %d, got %d", tt.hour, tt.want, w.Code)
			}
		})
	}
}

func TestTimeBased_CrossMidnightWindow(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("night-block", "time_based", TimeBasedConfig{
		BlockStartHour: 22,
		BlockEndHour:   6,
		Timezone:       "UTC",
	})

	engine := NewEngine(loader)

	tests := []struct {
		name string
		hour int
		want int
	}{
		{"before block start", 21, http.StatusOK},
		{"at block start", 22, http.StatusForbidden},
		{"late night", 23, http.StatusForbidden},
		{"midnight", 0, http.StatusForbidden},
		{"early morning", 3, http.StatusForbidden},
		{"at block end", 6, http.StatusOK},
		{"after block end", 7, http.StatusOK},
		{"midday", 12, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine.nowFunc = func() time.Time {
				return time.Date(2025, 1, 15, tt.hour, 0, 0, 0, time.UTC)
			}
			handler := engine.Middleware(okHandler())
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("hour %d: expected %d, got %d", tt.hour, tt.want, w.Code)
			}
		})
	}
}

func TestTimeBased_WithTimezone(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("est-block", "time_based", TimeBasedConfig{
		BlockStartHour: 22,
		BlockEndHour:   6,
		Timezone:       "America/New_York",
	})

	engine := NewEngine(loader)
	// 3:00 UTC = 22:00 EST (in January, UTC-5)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 3, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (22:00 EST), got %d", w.Code)
	}
}

func TestTimeBased_InvalidTimezone_Passes(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("bad-tz", "time_based", TimeBasedConfig{
		BlockStartHour: 0,
		BlockEndHour:   24,
		Timezone:       "Invalid/Zone",
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (invalid timezone should be lenient), got %d", w.Code)
	}
}

// --- Content Policy Tests ---

func TestContent_BlocksMatchingPattern(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-dangerous", "content", ContentConfig{
		BlockedPatterns: []string{"delete_all", "drop_table"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())

	body := toolCallBodyWithArgs("execute_sql", map[string]any{"query": "DROP_TABLE users"})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for drop_table content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContent_AllowsSafeContent(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-dangerous", "content", ContentConfig{
		BlockedPatterns: []string{"delete_all", "drop_table"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())

	body := toolCallBodyWithArgs("read_data", map[string]any{"table": "users"})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for safe content, got %d", w.Code)
	}
}

func TestContent_CaseInsensitive(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-dangerous", "content", ContentConfig{
		BlockedPatterns: []string{"delete_all"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())

	body := toolCallBodyWithArgs("run", map[string]any{"cmd": "DELETE_ALL"})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for case-insensitive match, got %d", w.Code)
	}
}

func TestContent_MultiplePatterns(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-dangerous", "content", ContentConfig{
		BlockedPatterns: []string{"rm -rf", "format c:", "shutdown"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	tests := []struct {
		name string
		body string
		want int
	}{
		{"matches first", `{"cmd": "rm -rf /"}`, http.StatusForbidden},
		{"matches second", `{"cmd": "format c: /q"}`, http.StatusForbidden},
		{"matches third", `{"cmd": "shutdown now"}`, http.StatusForbidden},
		{"no match", `{"cmd": "ls -la"}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := engine.Middleware(okHandler())
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, w.Code)
			}
		})
	}
}

func TestContent_EmptyPatterns_AllowsAll(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("empty-content", "content", ContentConfig{
		BlockedPatterns: []string{},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())
	body := toolCallBodyWithArgs("delete_everything", map[string]any{"all": true})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with empty patterns, got %d", w.Code)
	}
}

// --- Rate-of-Change Policy Tests ---

func TestRateOfChange_AllowsUnderLimit(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 5,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	now := time.Now()
	engine.nowFunc = func() time.Time { return now }

	handler := engine.Middleware(okHandler())
	keyID := uuid.New()

	for i := range 5 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_record")))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestRateOfChange_BlocksOverLimit(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 3,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	now := time.Now()
	engine.nowFunc = func() time.Time { return now }

	handler := engine.Middleware(okHandler())
	keyID := uuid.New()

	for range 3 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_record")))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 under limit, got %d", w.Code)
		}
	}

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_record")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 over limit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRateOfChange_WindowExpiry(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 2,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	now := time.Now()
	engine.nowFunc = func() time.Time { return now }

	handler := engine.Middleware(okHandler())
	keyID := uuid.New()

	for range 2 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("update_record")))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Advance time past the window
	now = now.Add(61 * time.Second)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("update_record")))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after window expired, got %d", w.Code)
	}
}

func TestRateOfChange_ReadOperationsNotCounted(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 1,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	now := time.Now()
	engine.nowFunc = func() time.Time { return now }

	handler := engine.Middleware(okHandler())
	keyID := uuid.New()

	for range 10 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("get_data")))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for read operations, got %d", w.Code)
		}
	}
}

func TestRateOfChange_PerKeyTracking(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 2,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	now := time.Now()
	engine.nowFunc = func() time.Time { return now }

	handler := engine.Middleware(okHandler())
	key1 := uuid.New()
	key2 := uuid.New()

	for range 2 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_item")))
		req = withAPIKey(req, store.ApiKey{ID: key1})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// key1 is over limit
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_item")))
	req = withAPIKey(req, store.ApiKey{ID: key1})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("key1 should be blocked, got %d", w.Code)
	}

	// key2 should still work
	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_item")))
	req = withAPIKey(req, store.ApiKey{ID: key2})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("key2 should be allowed, got %d", w.Code)
	}
}

func TestRateOfChange_NoAPIKey_Passes(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("write-limit", "rate_of_change", RateOfChangeConfig{
		MaxWritesPerWindow: 1,
		WindowSeconds:      60,
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())

	for range 5 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_all")))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 without API key, got %d", w.Code)
		}
	}
}

// --- IsWriteOperation Tests ---

func TestIsWriteOperation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"delete tool", toolCallBody("delete_record"), true},
		{"update tool", toolCallBody("update_user"), true},
		{"create tool", toolCallBody("create_item"), true},
		{"write tool", toolCallBody("write_file"), true},
		{"read tool", toolCallBody("read_data"), false},
		{"get tool", toolCallBody("get_weather"), false},
		{"list tool", toolCallBody("list_items"), false},
		{"body with delete keyword", `{"query": "delete from users"}`, true},
		{"body with drop keyword", `{"query": "drop table users"}`, true},
		{"safe body", `{"query": "select * from users"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWriteOperation(tt.body)
			if got != tt.want {
				t.Fatalf("isWriteOperation(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// --- Middleware integration tests ---

func TestMiddleware_NoPolicies_Passes(t *testing.T) {
	loader := &mockLoader{}
	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("anything")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with no policies, got %d", w.Code)
	}
}

func TestMiddleware_MultiplePolicies_FirstViolationBlocks(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("night-block", "time_based", TimeBasedConfig{
		BlockStartHour: 22,
		BlockEndHour:   6,
	})
	loader.addPolicy("content-block", "content", ContentConfig{
		BlockedPatterns: []string{"danger"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 23, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("safe_tool")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from time policy, got %d", w.Code)
	}
}

func TestMiddleware_BodyPreservedForDownstream(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("content-check", "content", ContentConfig{
		BlockedPatterns: []string{"never_matches_this_string_xyz"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	var downstream map[string]any
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&downstream)
		w.WriteHeader(http.StatusOK)
	})

	handler := engine.Middleware(inner)
	body := toolCallBody("read_data")
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if downstream["method"] != "tools/call" {
		t.Fatalf("downstream should read body, got method=%v", downstream["method"])
	}
}

func TestMiddleware_DeniedResponse_Format(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-all-hours", "time_based", TimeBasedConfig{
		BlockStartHour: 0,
		BlockEndHour:   24,
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
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
	if body.Error.Code != -32002 {
		t.Fatalf("expected error code -32002, got %d", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "policy violation") {
		t.Fatalf("expected 'policy violation' in message, got %q", body.Error.Message)
	}
}

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("content-block", "content", ContentConfig{
		BlockedPatterns: []string{"blocked_pattern"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("safe_tool")))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		}()
		go func() {
			defer wg.Done()
			body := `{"data": "blocked_pattern in here"}`
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

// --- Edge cases ---

func TestTimeBased_EqualStartEnd_NoBlock(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("zero-window", "time_based", TimeBasedConfig{
		BlockStartHour: 12,
		BlockEndHour:   12,
	})

	engine := NewEngine(loader)
	engine.nowFunc = func() time.Time {
		return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	}

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for equal start/end, got %d", w.Code)
	}
}

func TestContent_BlocksToolName(t *testing.T) {
	loader := &mockLoader{}
	loader.addPolicy("block-tool", "content", ContentConfig{
		BlockedPatterns: []string{"delete_all"},
	})

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("delete_all_records")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when tool name contains blocked pattern, got %d", w.Code)
	}
}

func TestInvalidConfig_Lenient(t *testing.T) {
	loader := &mockLoader{}
	loader.mu.Lock()
	loader.policies = append(loader.policies, store.Policy{
		ID:         uuid.New(),
		Name:       "bad-config",
		PolicyType: "time_based",
		Enabled:    true,
		Config:     json.RawMessage(`{invalid json`),
	})
	loader.mu.Unlock()

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid config (lenient), got %d", w.Code)
	}
}

func TestUnknownPolicyType_Passes(t *testing.T) {
	loader := &mockLoader{}
	loader.mu.Lock()
	loader.policies = append(loader.policies, store.Policy{
		ID:         uuid.New(),
		Name:       "future-type",
		PolicyType: "unknown_type",
		Enabled:    true,
		Config:     json.RawMessage(`{}`),
	})
	loader.mu.Unlock()

	engine := NewEngine(loader)
	engine.nowFunc = time.Now

	handler := engine.Middleware(okHandler())
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(toolCallBody("test")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for unknown policy type, got %d", w.Code)
	}
}
