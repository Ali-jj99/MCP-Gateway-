// Package health provides liveness and readiness probe endpoints.
package health

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

type Checker struct {
	mu    sync.RWMutex
	db    *sql.DB
	ready atomic.Bool
}

func NewChecker(db *sql.DB) *Checker {
	return &Checker{db: db}
}

func (c *Checker) SetDB(db *sql.DB) {
	c.mu.Lock()
	c.db = db
	c.mu.Unlock()
}

func (c *Checker) getDB() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}

func (c *Checker) SetReady(v bool) {
	c.ready.Store(v)
}

func (c *Checker) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (c *Checker) Readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !c.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
		return
	}

	if db := c.getDB(); db != nil {
		if err := db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready", "reason": "database"})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
