package ratelimit_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/ratelimit"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockLoader struct {
	mu      sync.Mutex
	configs map[uuid.UUID]store.GetRateLimitByKeyIDRow
	calls   atomic.Int64
}

func newMockLoader() *mockLoader {
	return &mockLoader{configs: make(map[uuid.UUID]store.GetRateLimitByKeyIDRow)}
}

func (m *mockLoader) GetRateLimitByKeyID(_ context.Context, apiKeyID uuid.UUID) (store.GetRateLimitByKeyIDRow, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.configs[apiKeyID]
	if !ok {
		return store.GetRateLimitByKeyIDRow{}, sql.ErrNoRows
	}
	return row, nil
}

func (m *mockLoader) set(keyID uuid.UUID, rpm, burst int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[keyID] = store.GetRateLimitByKeyIDRow{
		ID:             uuid.New(),
		ApiKeyID:       keyID,
		RequestsPerMin: rpm,
		BurstSize:      burst,
	}
}

// --- Token bucket core tests ---

func TestAllow_WithinBurst(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 5)

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	for i := range 5 {
		ok, _ := lim.Allow(context.Background(), keyID)
		if !ok {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}
}

func TestAllow_BurstExceeded(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 3)

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	for range 3 {
		ok, _ := lim.Allow(context.Background(), keyID)
		if !ok {
			t.Fatal("requests within burst should be allowed")
		}
	}

	ok, retryAfter := lim.Allow(context.Background(), keyID)
	if ok {
		t.Fatal("request exceeding burst should be denied")
	}
	if retryAfter <= 0 {
		t.Fatal("retryAfter should be positive")
	}
}

func TestAllow_RefillsOverTime(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{
		RequestsPerMin: 60,
		BurstSize:      2,
	})

	keyID := uuid.New()

	ok, _ := lim.Allow(context.Background(), keyID)
	if !ok {
		t.Fatal("first request should be allowed")
	}
	ok, _ = lim.Allow(context.Background(), keyID)
	if !ok {
		t.Fatal("second request should be allowed")
	}

	ok, _ = lim.Allow(context.Background(), keyID)
	if ok {
		t.Fatal("third request should be denied (burst=2)")
	}

	// 60 RPM = 1 token/second. Wait for refill.
	time.Sleep(1100 * time.Millisecond)

	ok, _ = lim.Allow(context.Background(), keyID)
	if !ok {
		t.Fatal("request should be allowed after token refill")
	}
}

func TestAllow_DefaultConfigWhenNoDBEntry(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	// Don't set any config — should use defaults.

	defaultCfg := ratelimit.Config{RequestsPerMin: 120, BurstSize: 2}
	lim := ratelimit.NewLimiter(loader, defaultCfg)

	for range 2 {
		ok, _ := lim.Allow(context.Background(), keyID)
		if !ok {
			t.Fatal("should allow within default burst")
		}
	}

	ok, _ := lim.Allow(context.Background(), keyID)
	if ok {
		t.Fatal("should deny beyond default burst")
	}
}

func TestAllow_DifferentKeysIndependent(t *testing.T) {
	loader := newMockLoader()
	key1 := uuid.New()
	key2 := uuid.New()
	loader.set(key1, 60, 1)
	loader.set(key2, 60, 1)

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	ok, _ := lim.Allow(context.Background(), key1)
	if !ok {
		t.Fatal("key1 first request should be allowed")
	}
	ok, _ = lim.Allow(context.Background(), key1)
	if ok {
		t.Fatal("key1 second request should be denied")
	}

	ok, _ = lim.Allow(context.Background(), key2)
	if !ok {
		t.Fatal("key2 should be independent and allowed")
	}
}

func TestAllow_RetryAfterCalculation(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 1) // 1 token/sec refill, burst 1

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	lim.Allow(context.Background(), keyID) // consume the single token
	ok, retryAfter := lim.Allow(context.Background(), keyID)
	if ok {
		t.Fatal("should be denied")
	}
	if retryAfter < time.Second {
		t.Fatalf("retryAfter should be >= 1s, got %v", retryAfter)
	}
}

func TestAllow_TokensDoNotExceedCapacity(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{
		RequestsPerMin: 600,
		BurstSize:      3,
	})
	keyID := uuid.New()

	// Use all tokens.
	for range 3 {
		lim.Allow(context.Background(), keyID)
	}

	// Wait long enough that many tokens would accumulate if uncapped.
	time.Sleep(500 * time.Millisecond) // 600 RPM = 10/sec => ~5 tokens if uncapped

	// Should only be able to use burst-size (3) tokens.
	allowed := 0
	for range 10 {
		ok, _ := lim.Allow(context.Background(), keyID)
		if ok {
			allowed++
		}
	}
	if allowed > 3 {
		t.Fatalf("tokens should not exceed burst capacity 3, but got %d allowed", allowed)
	}
}

func TestSetConfig_OverridesDBLookup(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 10) // DB says burst=10

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)
	lim.SetConfig(keyID, ratelimit.Config{RequestsPerMin: 60, BurstSize: 2})

	for range 2 {
		ok, _ := lim.Allow(context.Background(), keyID)
		if !ok {
			t.Fatal("should allow within overridden burst=2")
		}
	}
	ok, _ := lim.Allow(context.Background(), keyID)
	if ok {
		t.Fatal("should deny beyond overridden burst=2")
	}
}

func TestCleanup_RemovesIdleBuckets(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{RequestsPerMin: 60, BurstSize: 5})
	keyID := uuid.New()

	lim.Allow(context.Background(), keyID)

	// Cleanup should not remove recently used buckets.
	lim.Cleanup()
	ok, _ := lim.Allow(context.Background(), keyID)
	if !ok {
		t.Fatal("bucket should still exist after cleanup of active bucket")
	}
}

// --- Concurrent access tests ---

func TestAllow_ConcurrentAccess(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 600, 100)

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	var wg sync.WaitGroup
	var allowed atomic.Int64
	var denied atomic.Int64

	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := lim.Allow(context.Background(), keyID)
			if ok {
				allowed.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	total := allowed.Load() + denied.Load()
	if total != 200 {
		t.Fatalf("expected 200 total, got %d", total)
	}
	if allowed.Load() > 100 {
		t.Fatalf("should not allow more than burst (100), got %d", allowed.Load())
	}
	if allowed.Load() == 0 {
		t.Fatal("should allow at least some requests")
	}
}

func TestAllow_ConcurrentMultipleKeys(t *testing.T) {
	loader := newMockLoader()
	keys := make([]uuid.UUID, 10)
	for i := range keys {
		keys[i] = uuid.New()
		loader.set(keys[i], 60, 5)
	}

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	var wg sync.WaitGroup
	for _, keyID := range keys {
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				lim.Allow(context.Background(), keyID)
			}()
		}
	}
	wg.Wait()
}

// --- Stress test ---

func TestAllow_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 6000, 100) // 100/sec rate, burst 100

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	var wg sync.WaitGroup
	var allowed atomic.Int64

	goroutines := 50
	requestsPerGoroutine := 100

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requestsPerGoroutine {
				ok, _ := lim.Allow(context.Background(), keyID)
				if ok {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	a := allowed.Load()
	if a > int64(100+6000) {
		t.Fatalf("allowed too many: %d", a)
	}
	if a < 100 {
		t.Fatalf("should allow at least burst amount, got %d", a)
	}
	t.Logf("stress test: %d/%d requests allowed", a, goroutines*requestsPerGoroutine)
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

func TestMiddleware_AllowsNormalTraffic(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{RequestsPerMin: 60, BurstSize: 10})
	handler := lim.Middleware(okHandler())

	keyID := uuid.New()
	for range 5 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}
}

func TestMiddleware_Returns429WhenExceeded(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{RequestsPerMin: 60, BurstSize: 2})
	handler := lim.Middleware(okHandler())

	keyID := uuid.New()

	// Exhaust burst.
	for range 2 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// This one should be rate limited.
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After not an integer: %q", retryAfter)
	}
	if secs < 1 {
		t.Fatalf("Retry-After should be >= 1, got %d", secs)
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
	if body.Error.Message != "rate limit exceeded" {
		t.Fatalf("expected 'rate limit exceeded', got %q", body.Error.Message)
	}
}

func TestMiddleware_PassesThroughWithoutAPIKey(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{RequestsPerMin: 1, BurstSize: 1})
	handler := lim.Middleware(okHandler())

	// No API key in context — middleware should pass through.
	for range 5 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 without API key, got %d", w.Code)
		}
	}
}

func TestMiddleware_UsesDBConfig(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 3)

	lim := ratelimit.NewLimiter(loader, ratelimit.Config{RequestsPerMin: 60, BurstSize: 100})
	handler := lim.Middleware(okHandler())

	// DB config says burst=3, default says burst=100. Should use DB config.
	for range 3 {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
		req = withAPIKey(req, store.ApiKey{ID: keyID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req = withAPIKey(req, store.ApiKey{ID: keyID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after DB-configured burst, got %d", w.Code)
	}
}

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	lim := ratelimit.NewLimiterWithDefaults(ratelimit.Config{RequestsPerMin: 600, BurstSize: 50})
	handler := lim.Middleware(okHandler())

	keyID := uuid.New()
	var wg sync.WaitGroup
	var ok200 atomic.Int64
	var ok429 atomic.Int64

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
			req = withAPIKey(req, store.ApiKey{ID: keyID})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			switch w.Code {
			case 200:
				ok200.Add(1)
			case 429:
				ok429.Add(1)
			}
		}()
	}
	wg.Wait()

	total := ok200.Load() + ok429.Load()
	if total != 100 {
		t.Fatalf("expected 100 total responses, got %d", total)
	}
	if ok200.Load() > 50 {
		t.Fatalf("should not allow more than burst=50, got %d 200s", ok200.Load())
	}
	if ok429.Load() == 0 {
		t.Fatal("expected some 429s")
	}
	t.Logf("concurrent middleware: %d allowed, %d denied", ok200.Load(), ok429.Load())
}

func TestMiddleware_ConfigCached(t *testing.T) {
	loader := newMockLoader()
	keyID := uuid.New()
	loader.set(keyID, 60, 10)

	lim := ratelimit.NewLimiter(loader, ratelimit.DefaultConfig)

	for range 5 {
		lim.Allow(context.Background(), keyID)
	}

	if loader.calls.Load() != 1 {
		t.Fatalf("expected 1 DB call (cached), got %d", loader.calls.Load())
	}
}
