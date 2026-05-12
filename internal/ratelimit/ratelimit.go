package ratelimit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type ConfigLoader interface {
	GetRateLimitByKeyID(ctx context.Context, apiKeyID uuid.UUID) (store.GetRateLimitByKeyIDRow, error)
}

type Config struct {
	RequestsPerMin int
	BurstSize      int
}

var DefaultConfig = Config{
	RequestsPerMin: 60,
	BurstSize:      10,
}

type bucket struct {
	mu        sync.Mutex
	tokens    float64
	capacity  float64
	rate      float64 // tokens per second
	lastFill  time.Time
	retryAt   time.Time
}

func (b *bucket) allow(now time.Time) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = math.Min(b.capacity, b.tokens+elapsed*b.rate)
	b.lastFill = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	wait := time.Duration((1-b.tokens)/b.rate*1000) * time.Millisecond
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}

type Limiter struct {
	loader     ConfigLoader
	mu         sync.RWMutex
	buckets    map[uuid.UUID]*bucket
	configs    map[uuid.UUID]Config
	defaultCfg Config
	now        func() time.Time
}

func NewLimiter(loader ConfigLoader, defaultCfg Config) *Limiter {
	return &Limiter{
		loader:     loader,
		buckets:    make(map[uuid.UUID]*bucket),
		configs:    make(map[uuid.UUID]Config),
		defaultCfg: defaultCfg,
		now:        time.Now,
	}
}

func (l *Limiter) loadConfig(ctx context.Context, keyID uuid.UUID) Config {
	l.mu.RLock()
	if cfg, ok := l.configs[keyID]; ok {
		l.mu.RUnlock()
		return cfg
	}
	l.mu.RUnlock()

	if l.loader != nil {
		row, err := l.loader.GetRateLimitByKeyID(ctx, keyID)
		if err == nil {
			cfg := Config{
				RequestsPerMin: int(row.RequestsPerMin),
				BurstSize:      int(row.BurstSize),
			}
			l.mu.Lock()
			l.configs[keyID] = cfg
			l.mu.Unlock()
			return cfg
		}
	}

	return l.defaultCfg
}

func (l *Limiter) getBucket(ctx context.Context, keyID uuid.UUID) *bucket {
	l.mu.RLock()
	b, ok := l.buckets[keyID]
	l.mu.RUnlock()
	if ok {
		return b
	}

	cfg := l.loadConfig(ctx, keyID)
	rate := float64(cfg.RequestsPerMin) / 60.0

	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.buckets[keyID]; ok {
		return b
	}

	b = &bucket{
		tokens:   float64(cfg.BurstSize),
		capacity: float64(cfg.BurstSize),
		rate:     rate,
		lastFill: l.now(),
	}
	l.buckets[keyID] = b
	return b
}

func (l *Limiter) Allow(ctx context.Context, keyID uuid.UUID) (bool, time.Duration) {
	b := l.getBucket(ctx, keyID)
	return b.allow(l.now())
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.APIKeyFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		allowed, retryAfter := l.Allow(r.Context(), key.ID)
		if !allowed {
			slog.Warn("rate limit exceeded",
				"api_key_id", key.ID,
				"retry_after", retryAfter,
			)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32001,
					"message": "rate limit exceeded",
				},
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	for id, b := range l.buckets {
		b.mu.Lock()
		idle := now.Sub(b.lastFill)
		b.mu.Unlock()
		if idle > 10*time.Minute {
			delete(l.buckets, id)
			delete(l.configs, id)
		}
	}
}

// SetConfig injects a rate limit config for a key, bypassing DB lookup. Useful for testing.
func (l *Limiter) SetConfig(keyID uuid.UUID, cfg Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configs[keyID] = cfg
	delete(l.buckets, keyID)
}

// stubLoader satisfies ConfigLoader for cases when no DB is available.
type stubLoader struct{}

func (stubLoader) GetRateLimitByKeyID(_ context.Context, _ uuid.UUID) (store.GetRateLimitByKeyIDRow, error) {
	return store.GetRateLimitByKeyIDRow{}, sql.ErrNoRows
}

// NewLimiterWithDefaults creates a limiter with no DB, using only the default config.
func NewLimiterWithDefaults(cfg Config) *Limiter {
	return NewLimiter(stubLoader{}, cfg)
}
