package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ali-jj99/mcp-gateway/internal/proxy"
)

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newMCPServer() *httptest.Server {
	sessionID := "test-session-123"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonrpcRequest
		_ = json.Unmarshal(body, &req)

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2025-11-25",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "test-server", "version": "1.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "echo",
							"description": "echoes input",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
						},
					},
				},
			})
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "echoed"}},
				},
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			})
		}
	}))
}

func postJSON(t *testing.T, url string, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response) jsonrpcResponse {
	t.Helper()
	defer resp.Body.Close()
	var rpcResp jsonrpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}
	return rpcResp
}

func TestProxyForwardsInitialize(t *testing.T) {
	upstream := newMCPServer()
	defer upstream.Close()

	h, err := proxy.NewHandler(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}
	}`, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("expected Mcp-Session-Id header in response")
	}
	if sessionID != "test-session-123" {
		t.Fatalf("expected session ID 'test-session-123', got %q", sessionID)
	}

	rpcResp := decodeResponse(t, resp)
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
}

func TestProxyForwardsToolsList(t *testing.T) {
	upstream := newMCPServer()
	defer upstream.Close()

	h, err := proxy.NewHandler(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}`,
		map[string]string{"Mcp-Session-Id": "test-session-123"})

	rpcResp := decodeResponse(t, resp)
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("expected 1 tool named 'echo', got %+v", result.Tools)
	}
}

func TestProxyForwardsToolsCall(t *testing.T) {
	upstream := newMCPServer()
	defer upstream.Close()

	h, err := proxy.NewHandler(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": {"name": "echo", "arguments": {"message": "hello"}}
	}`, map[string]string{"Mcp-Session-Id": "test-session-123"})

	rpcResp := decodeResponse(t, resp)
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "echoed" {
		t.Fatalf("expected 'echoed', got %+v", result.Content)
	}
}

func TestProxyForwardsInitializedNotification(t *testing.T) {
	upstream := newMCPServer()
	defer upstream.Close()

	h, err := proxy.NewHandler(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{"jsonrpc": "2.0", "method": "notifications/initialized"}`,
		map[string]string{"Mcp-Session-Id": "test-session-123"})
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
}

func TestProxyReturnsErrorForUnknownMethod(t *testing.T) {
	upstream := newMCPServer()
	defer upstream.Close()

	h, err := proxy.NewHandler(upstream.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{"jsonrpc": "2.0", "id": 99, "method": "nonexistent"}`,
		map[string]string{"Mcp-Session-Id": "test-session-123"})

	rpcResp := decodeResponse(t, resp)
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if rpcResp.Error.Code != -32601 {
		t.Fatalf("expected error code -32601, got %d", rpcResp.Error.Code)
	}
}

func TestProxyUpstreamDown(t *testing.T) {
	h, err := proxy.NewHandler("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer gw.Close()

	resp := postJSON(t, gw.URL, `{"jsonrpc": "2.0", "id": 1, "method": "initialize"}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}
