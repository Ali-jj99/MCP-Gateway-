package audit_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/audit"
	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockQuerier struct {
	mu      sync.Mutex
	logs    []store.InsertAuditLogParams
	inserts atomic.Int64
	err     error
}

func (m *mockQuerier) InsertAuditLog(_ context.Context, arg store.InsertAuditLogParams) error {
	m.inserts.Add(1)
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	m.logs = append(m.logs, arg)
	m.mu.Unlock()
	return nil
}

func (m *mockQuerier) getLogs() []store.InsertAuditLogParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.InsertAuditLogParams, len(m.logs))
	copy(cp, m.logs)
	return cp
}

func (m *mockQuerier) CreateAPIKey(_ context.Context, _ store.CreateAPIKeyParams) (store.ApiKey, error) {
	return store.ApiKey{}, nil
}
func (m *mockQuerier) DeleteAPIKey(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) GetAPIKeyByHash(_ context.Context, _ string) (store.ApiKey, error) {
	return store.ApiKey{}, sql.ErrNoRows
}
func (m *mockQuerier) ListAPIKeys(_ context.Context) ([]store.ListAPIKeysRow, error) {
	return nil, nil
}
func (m *mockQuerier) ListAuditLogs(_ context.Context, _ store.ListAuditLogsParams) ([]store.ListAuditLogsRow, error) {
	return nil, nil
}
func (m *mockQuerier) RevokeAPIKey(_ context.Context, _ uuid.UUID) error { return nil }

// --- Async behavior tests ---

func TestLogger_AsyncProcessing(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)
	defer logger.Close()

	logger.Log(audit.Entry{
		ApiKeyID:   uuid.New(),
		Action:     "POST",
		Resource:   "/mcp",
		StatusCode: 200,
		LatencyMs:  42,
		ToolName:   "test_tool",
	})

	deadline := time.After(2 * time.Second)
	for mock.inserts.Load() != 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for async insert")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	logs := mock.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ToolName != "test_tool" {
		t.Fatalf("expected tool_name 'test_tool', got %q", logs[0].ToolName)
	}
	if logs[0].StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", logs[0].StatusCode)
	}
}

func TestLogger_NonBlocking(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)
	defer logger.Close()

	done := make(chan struct{})
	go func() {
		logger.Log(audit.Entry{ToolName: "fast"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Log() blocked for too long")
	}
}

func TestLogger_ProcessesAllBeforeClose(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)

	for i := range 50 {
		logger.Log(audit.Entry{ToolName: fmt.Sprintf("tool_%d", i)})
	}

	logger.Close()

	if got := mock.inserts.Load(); got != 50 {
		t.Fatalf("expected 50 inserts after Close(), got %d", got)
	}
}

// --- Buffer overflow tests ---

func TestLogger_BufferOverflow_DropsEntries(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 2)

	// Block the processor by not consuming
	// Fill the buffer (size=2) plus send extras that should be dropped
	blocker := make(chan struct{})
	mock.err = nil

	// Temporarily replace the mock to block processing
	blockMock := &blockingQuerier{unblock: blocker}
	blockLogger := audit.NewLogger(blockMock, 2)

	// Fill buffer
	blockLogger.Log(audit.Entry{ToolName: "a"})
	blockLogger.Log(audit.Entry{ToolName: "b"})

	// Wait for processor to pick up the first entry (it blocks on insert)
	deadline := time.After(time.Second)
	for blockMock.blocked.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for processor to block")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Now channel has 1 entry ("b"), processor is blocked on "a"
	// Fill the remaining slot
	blockLogger.Log(audit.Entry{ToolName: "c"})

	// This one should be dropped (buffer full + processor blocked)
	accepted := blockLogger.Log(audit.Entry{ToolName: "d"})
	if accepted {
		t.Fatal("expected entry to be dropped when buffer is full")
	}

	if blockLogger.Dropped() != 1 {
		t.Fatalf("expected 1 dropped, got %d", blockLogger.Dropped())
	}

	close(blocker)
	blockLogger.Close()
	logger.Close()
}

type blockingQuerier struct {
	mockQuerier
	unblock chan struct{}
	blocked atomic.Int64
}

func (b *blockingQuerier) InsertAuditLog(_ context.Context, arg store.InsertAuditLogParams) error {
	b.blocked.Add(1)
	<-b.unblock
	b.mu.Lock()
	b.logs = append(b.logs, arg)
	b.mu.Unlock()
	return nil
}

// --- Middleware tests ---

func TestMiddleware_CapturesRequestAndResponse(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)

	handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))

	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_weather"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	logger.Close()

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logs := mock.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	entry := logs[0]
	if entry.ToolName != "get_weather" {
		t.Fatalf("expected tool_name 'get_weather', got %q", entry.ToolName)
	}
	if entry.Action != "POST" {
		t.Fatalf("expected action 'POST', got %q", entry.Action)
	}
	if entry.Resource != "/mcp" {
		t.Fatalf("expected resource '/mcp', got %q", entry.Resource)
	}
	if entry.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", entry.StatusCode)
	}
	if entry.Ip != "10.0.0.1" {
		t.Fatalf("expected IP '10.0.0.1', got %q", entry.Ip)
	}
	if entry.RequestBody != body {
		t.Fatalf("request body mismatch: got %q", entry.RequestBody)
	}
	if entry.ResponseBody != `{"result":"ok"}` {
		t.Fatalf("response body mismatch: got %q", entry.ResponseBody)
	}
	if entry.LatencyMs < 0 {
		t.Fatalf("expected non-negative latency, got %d", entry.LatencyMs)
	}
}

func TestMiddleware_CapturesAPIKeyFromContext(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)

	keyID := uuid.New()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := logger.Middleware(inner)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	ctx := context.WithValue(req.Context(), auth.APIKeyContextKey, store.ApiKey{ID: keyID})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	logger.Close()

	logs := mock.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ApiKeyID != keyID {
		t.Fatalf("expected api_key_id %s, got %s", keyID, logs[0].ApiKeyID)
	}
}

func TestMiddleware_CapturesStatusCode(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)

	handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	logger.Close()

	logs := mock.getLogs()
	if logs[0].StatusCode != 502 {
		t.Fatalf("expected status 502, got %d", logs[0].StatusCode)
	}
}

func TestMiddleware_MethodExtraction(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantTool string
	}{
		{
			name:     "tools/call extracts param name",
			body:     `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"read_file"}}`,
			wantTool: "read_file",
		},
		{
			name:     "tools/list uses method",
			body:     `{"jsonrpc":"2.0","method":"tools/list"}`,
			wantTool: "tools/list",
		},
		{
			name:     "resources/read uses method",
			body:     `{"jsonrpc":"2.0","method":"resources/read","params":{"uri":"file:///tmp"}}`,
			wantTool: "resources/read",
		},
		{
			name:     "invalid json returns empty",
			body:     `not json`,
			wantTool: "",
		},
		{
			name:     "empty body returns empty",
			body:     "",
			wantTool: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockQuerier{}
			logger := audit.NewLogger(mock, 100)

			handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			logger.Close()

			logs := mock.getLogs()
			if logs[0].ToolName != tt.wantTool {
				t.Fatalf("expected tool_name %q, got %q", tt.wantTool, logs[0].ToolName)
			}
		})
	}
}

func TestMiddleware_DoesNotBreakDownstreamBodyRead(t *testing.T) {
	mock := &mockQuerier{}
	logger := audit.NewLogger(mock, 100)

	var downstream map[string]any
	handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&downstream)
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"test"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	logger.Close()

	if downstream["method"] != "tools/call" {
		t.Fatalf("downstream handler should still read body, got method=%v", downstream["method"])
	}
}

func TestMiddleware_ClientIP_Fallbacks(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		remote string
		want   string
	}{
		{"xff single", "1.2.3.4", "9.9.9.9:1234", "1.2.3.4"},
		{"xff multiple", "1.2.3.4, 5.6.7.8", "9.9.9.9:1234", "1.2.3.4"},
		{"no xff", "", "9.9.9.9:1234", "9.9.9.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockQuerier{}
			logger := audit.NewLogger(mock, 100)

			handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			req.RemoteAddr = tt.remote
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			logger.Close()

			logs := mock.getLogs()
			if logs[0].Ip != tt.want {
				t.Fatalf("expected IP %q, got %q", tt.want, logs[0].Ip)
			}
		})
	}
}

// --- Query filter tests (params struct construction) ---

func TestListAuditLogsParams_AllFilters(t *testing.T) {
	keyID := uuid.New()
	now := time.Now()
	params := store.ListAuditLogsParams{
		ApiKeyID:   uuid.NullUUID{UUID: keyID, Valid: true},
		ToolName:   sql.NullString{String: "read_file", Valid: true},
		StatusCode: sql.NullInt32{Int32: 200, Valid: true},
		StartTime:  sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		EndTime:    sql.NullTime{Time: now, Valid: true},
		PageLimit:  50,
	}

	if !params.ApiKeyID.Valid {
		t.Fatal("api_key_id filter should be set")
	}
	if params.ToolName.String != "read_file" {
		t.Fatal("tool_name filter mismatch")
	}
	if params.StatusCode.Int32 != 200 {
		t.Fatal("status_code filter mismatch")
	}
	if params.PageLimit != 50 {
		t.Fatal("page_limit mismatch")
	}
}

func TestListAuditLogsParams_NoFilters(t *testing.T) {
	params := store.ListAuditLogsParams{
		PageLimit: 100,
	}

	if params.ApiKeyID.Valid {
		t.Fatal("api_key_id should be null")
	}
	if params.ToolName.Valid {
		t.Fatal("tool_name should be null")
	}
	if params.StatusCode.Valid {
		t.Fatal("status_code should be null")
	}
	if params.StartTime.Valid {
		t.Fatal("start_time should be null")
	}
	if params.EndTime.Valid {
		t.Fatal("end_time should be null")
	}
}

func TestListAuditLogsParams_PartialFilters(t *testing.T) {
	params := store.ListAuditLogsParams{
		ToolName:  sql.NullString{String: "search", Valid: true},
		PageLimit: 25,
	}

	if params.ApiKeyID.Valid {
		t.Fatal("api_key_id should be null")
	}
	if !params.ToolName.Valid || params.ToolName.String != "search" {
		t.Fatal("tool_name filter mismatch")
	}
	if params.StatusCode.Valid {
		t.Fatal("status_code should be null")
	}
}
