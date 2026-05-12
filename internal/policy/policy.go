package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type Loader interface {
	ListEnabledPolicies(ctx context.Context) ([]store.Policy, error)
}

type TimeBasedConfig struct {
	BlockStartHour int    `json:"block_start_hour"`
	BlockEndHour   int    `json:"block_end_hour"`
	Timezone       string `json:"timezone"`
}

type ContentConfig struct {
	BlockedPatterns []string `json:"blocked_patterns"`
}

type RateOfChangeConfig struct {
	MaxWritesPerWindow int `json:"max_writes_per_window"`
	WindowSeconds      int `json:"window_seconds"`
}

type Engine struct {
	loader       Loader
	mu           sync.RWMutex
	policies     []store.Policy
	lastLoad     time.Time
	cacheTTL     time.Duration
	writeTracker *writeTracker
	nowFunc      func() time.Time
}

type writeTracker struct {
	mu      sync.Mutex
	windows map[uuid.UUID][]time.Time
}

func newWriteTracker() *writeTracker {
	return &writeTracker{
		windows: make(map[uuid.UUID][]time.Time),
	}
}

func (wt *writeTracker) record(keyID uuid.UUID, now time.Time) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.windows[keyID] = append(wt.windows[keyID], now)
}

func (wt *writeTracker) countInWindow(keyID uuid.UUID, windowStart time.Time) int {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	times := wt.windows[keyID]
	kept := times[:0]
	count := 0
	for _, t := range times {
		if !t.Before(windowStart) {
			kept = append(kept, t)
			count++
		}
	}
	wt.windows[keyID] = kept
	return count
}

func NewEngine(loader Loader) *Engine {
	return &Engine{
		loader:       loader,
		cacheTTL:     30 * time.Second,
		writeTracker: newWriteTracker(),
		nowFunc:      time.Now,
	}
}

func (e *Engine) loadPolicies(ctx context.Context) []store.Policy {
	e.mu.RLock()
	if time.Since(e.lastLoad) < e.cacheTTL && e.policies != nil {
		defer e.mu.RUnlock()
		return e.policies
	}
	e.mu.RUnlock()

	policies, err := e.loader.ListEnabledPolicies(ctx)
	if err != nil {
		slog.Error("failed to load policies", "error", err)
		e.mu.RLock()
		defer e.mu.RUnlock()
		return e.policies
	}

	e.mu.Lock()
	e.policies = policies
	e.lastLoad = e.nowFunc()
	e.mu.Unlock()
	return policies
}

const maxBodyRead = 64 * 1024

func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policies := e.loadPolicies(r.Context())
		if len(policies) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		body := readAndRestore(r)

		now := e.nowFunc()

		for _, p := range policies {
			if err := e.evaluate(r, p, body, now); err != nil {
				slog.Warn("policy denied",
					"policy", p.Name,
					"policy_type", p.PolicyType,
					"reason", err.Error(),
				)
				writePolicyDenied(w, p.Name, err.Error())
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (e *Engine) evaluate(r *http.Request, p store.Policy, body string, now time.Time) error {
	switch p.PolicyType {
	case "time_based":
		return e.evaluateTimeBased(p, now)
	case "content":
		return e.evaluateContent(p, body)
	case "rate_of_change":
		return e.evaluateRateOfChange(r, p, body, now)
	default:
		return nil
	}
}

func (e *Engine) evaluateTimeBased(p store.Policy, now time.Time) error {
	var cfg TimeBasedConfig
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		slog.Error("invalid time_based policy config", "policy", p.Name, "error", err)
		return nil
	}

	loc := time.UTC
	if cfg.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Timezone)
		if err != nil {
			slog.Error("invalid timezone in policy", "policy", p.Name, "timezone", cfg.Timezone)
			return nil
		}
	}

	hour := now.In(loc).Hour()

	if cfg.BlockStartHour <= cfg.BlockEndHour {
		if hour >= cfg.BlockStartHour && hour < cfg.BlockEndHour {
			return fmt.Errorf("access blocked during hours %d:00-%d:00 %s",
				cfg.BlockStartHour, cfg.BlockEndHour, loc)
		}
	} else {
		if hour >= cfg.BlockStartHour || hour < cfg.BlockEndHour {
			return fmt.Errorf("access blocked during hours %d:00-%d:00 %s",
				cfg.BlockStartHour, cfg.BlockEndHour, loc)
		}
	}
	return nil
}

func (e *Engine) evaluateContent(p store.Policy, body string) error {
	var cfg ContentConfig
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		slog.Error("invalid content policy config", "policy", p.Name, "error", err)
		return nil
	}

	lower := strings.ToLower(body)
	for _, pattern := range cfg.BlockedPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return fmt.Errorf("request contains blocked pattern: %s", pattern)
		}
	}
	return nil
}

var writeIndicators = []string{
	"delete", "drop", "remove", "update", "insert", "create",
	"write", "put", "post", "modify", "alter", "set",
}

func isWriteOperation(body string) bool {
	lower := strings.ToLower(body)

	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(body), &req) == nil && req.Params.Name != "" {
		toolLower := strings.ToLower(req.Params.Name)
		for _, ind := range writeIndicators {
			if strings.Contains(toolLower, ind) {
				return true
			}
		}
	}

	for _, ind := range writeIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}

func (e *Engine) evaluateRateOfChange(r *http.Request, p store.Policy, body string, now time.Time) error {
	if !isWriteOperation(body) {
		return nil
	}

	var cfg RateOfChangeConfig
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		slog.Error("invalid rate_of_change policy config", "policy", p.Name, "error", err)
		return nil
	}

	if cfg.MaxWritesPerWindow <= 0 || cfg.WindowSeconds <= 0 {
		return nil
	}

	key, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		return nil
	}

	windowStart := now.Add(-time.Duration(cfg.WindowSeconds) * time.Second)
	count := e.writeTracker.countInWindow(key.ID, windowStart)

	if count >= cfg.MaxWritesPerWindow {
		return fmt.Errorf("too many write operations: %d in %ds (limit %d)",
			count, cfg.WindowSeconds, cfg.MaxWritesPerWindow)
	}

	e.writeTracker.record(key.ID, now)
	return nil
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

func writePolicyDenied(w http.ResponseWriter, policyName, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    -32002,
			"message": fmt.Sprintf("policy violation: %s — %s", policyName, reason),
		},
	})
}
