package audit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type Entry struct {
	ApiKeyID     uuid.UUID
	Action       string
	Resource     string
	StatusCode   int
	LatencyMs    int64
	IP           string
	RequestBody  string
	ResponseBody string
	ToolName     string
}

type Logger struct {
	entries chan Entry
	q       store.Querier
	wg      sync.WaitGroup
	dropped atomic.Int64
}

func NewLogger(q store.Querier, bufferSize int) *Logger {
	l := &Logger{
		entries: make(chan Entry, bufferSize),
		q:       q,
	}
	l.wg.Add(1)
	go l.process()
	return l
}

func (l *Logger) Log(e Entry) bool {
	select {
	case l.entries <- e:
		return true
	default:
		l.dropped.Add(1)
		slog.Warn("audit log buffer full, dropping entry",
			"tool_name", e.ToolName,
			"api_key_id", e.ApiKeyID,
		)
		return false
	}
}

func (l *Logger) Dropped() int64 {
	return l.dropped.Load()
}

func (l *Logger) Close() {
	close(l.entries)
	l.wg.Wait()
}

func (l *Logger) process() {
	defer l.wg.Done()
	for e := range l.entries {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := l.q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ApiKeyID:     e.ApiKeyID,
			Action:       e.Action,
			Resource:     e.Resource,
			StatusCode:   int32(e.StatusCode),
			LatencyMs:    e.LatencyMs,
			Ip:           e.IP,
			RequestBody:  e.RequestBody,
			ResponseBody: e.ResponseBody,
			ToolName:     e.ToolName,
		})
		cancel()
		if err != nil {
			slog.Error("failed to insert audit log", "error", err, "tool_name", e.ToolName)
		}
	}
}
