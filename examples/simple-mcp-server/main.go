// Package main implements a reference MCP server over Streamable HTTP transport.
//
// It exposes three tools: echo, get_time, and add.
// The server speaks JSON-RPC 2.0 over HTTP POST on a single endpoint (/mcp).
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

const protocolVersion = "2025-11-25"

// --- JSON-RPC 2.0 types ---

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP types ---

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      implementation `json:"clientInfo"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Tools ---

var tools = []tool{
	{
		Name:        "echo",
		Description: "Returns the message you send it",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo back",
				},
			},
			"required": []string{"message"},
		},
	},
	{
		Name:        "get_time",
		Description: "Returns the current server time in RFC 3339 format",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "add",
		Description: "Adds two numbers and returns the result",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]any{
					"type":        "number",
					"description": "Second number",
				},
			},
			"required": []string{"a", "b"},
		},
	},
}

func callTool(name string, args map[string]any) toolResult {
	switch name {
	case "echo":
		msg, _ := args["message"].(string)
		return toolResult{Content: []contentBlock{{Type: "text", Text: msg}}}
	case "get_time":
		return toolResult{Content: []contentBlock{{Type: "text", Text: time.Now().Format(time.RFC3339)}}}
	case "add":
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return toolResult{Content: []contentBlock{{Type: "text", Text: fmt.Sprintf("%g", a+b)}}}
	default:
		return toolResult{
			Content: []contentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", name)}},
			IsError: true,
		}
	}
}

// --- Server ---

type server struct {
	mu       sync.Mutex
	sessions map[string]bool
}

func newServer() *server {
	return &server{sessions: make(map[string]bool)}
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req jsonrpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonrpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "invalid request: jsonrpc must be 2.0"},
		})
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		s.handleToolsList(w, r, req)
	case "tools/call":
		s.handleToolsCall(w, r, req)
	case "ping":
		writeJSON(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		})
	default:
		writeJSON(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		})
	}
}

func (s *server) handleInitialize(w http.ResponseWriter, req jsonrpcRequest) {
	var params initializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "invalid initialize params"},
			})
			return
		}
	}

	sessionID := generateSessionID()
	s.mu.Lock()
	s.sessions[sessionID] = true
	s.mu.Unlock()

	w.Header().Set("Mcp-Session-Id", sessionID)
	writeJSON(w, http.StatusOK, jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": implementation{
				Name:    "simple-mcp-server",
				Version: "0.1.0",
			},
		},
	})
}

func (s *server) requireSession(w http.ResponseWriter, r *http.Request, reqID any) bool {
	sid := r.Header.Get("Mcp-Session-Id")
	if sid == "" {
		writeJSON(w, http.StatusBadRequest, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      reqID,
			Error:   &rpcError{Code: -32600, Message: "missing Mcp-Session-Id header; call initialize first"},
		})
		return false
	}
	s.mu.Lock()
	valid := s.sessions[sid]
	s.mu.Unlock()
	if !valid {
		writeJSON(w, http.StatusBadRequest, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      reqID,
			Error:   &rpcError{Code: -32600, Message: "invalid session"},
		})
		return false
	}
	return true
}

func (s *server) handleToolsList(w http.ResponseWriter, r *http.Request, req jsonrpcRequest) {
	if !s.requireSession(w, r, req.ID) {
		return
	}
	writeJSON(w, http.StatusOK, jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	})
}

func (s *server) handleToolsCall(w http.ResponseWriter, r *http.Request, req jsonrpcRequest) {
	if !s.requireSession(w, r, req.ID) {
		return
	}

	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSON(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid params"},
		})
		return
	}

	result := callTool(params.Name, params.Arguments)
	writeJSON(w, http.StatusOK, jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}

	srv := newServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", srv.handleMCP)

	slog.Info("starting simple MCP server", "port", port, "endpoint", "/mcp")
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), mux); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
