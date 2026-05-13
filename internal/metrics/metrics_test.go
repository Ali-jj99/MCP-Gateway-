package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddleware_RecordsMetrics(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	count := testutil.ToFloat64(RequestsTotal.WithLabelValues("POST", "unknown", "200"))
	if count != 1 {
		t.Fatalf("expected request count 1, got %f", count)
	}
}

func TestMiddleware_RecordsErrors(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	count := testutil.ToFloat64(ErrorsTotal.WithLabelValues("POST", "unknown", "500"))
	if count != 1 {
		t.Fatalf("expected error count 1, got %f", count)
	}
}

func TestMiddleware_ActiveConnections(t *testing.T) {
	done := make(chan struct{})
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := testutil.ToFloat64(ActiveConnections)
		if count != 1 {
			t.Errorf("expected 1 active connection during request, got %f", count)
		}
		close(done)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	<-done

	count := testutil.ToFloat64(ActiveConnections)
	if count != 0 {
		t.Fatalf("expected 0 active connections after request, got %f", count)
	}
}

func TestHandler_ServesMetrics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
