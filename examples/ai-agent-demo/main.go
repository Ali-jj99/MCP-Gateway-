package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	gatewayURL      = "http://localhost:8080/mcp"
	protocolVersion = "2025-11-25"
)

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpClient struct {
	apiKey    string
	sessionID string
	httpC     *http.Client
	nextID    int
}

func newMCPClient(apiKey string) *mcpClient {
	return &mcpClient{
		apiKey: apiKey,
		httpC:  &http.Client{Timeout: 10 * time.Second},
		nextID: 1,
	}
}

func (c *mcpClient) send(method string, params any, includeAuth bool) (*http.Response, *jsonrpcResponse, error) {
	id := c.nextID
	c.nextID++

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if method == "notifications/initialized" {
		req.ID = nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, gatewayURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if includeAuth && c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpC.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("http do: %w", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, nil, fmt.Errorf("read body: %w", err)
	}

	if len(respBody) == 0 {
		return resp, nil, nil
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return resp, nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}
	return resp, &rpcResp, nil
}

func header(text string) {
	line := strings.Repeat("=", len(text)+4)
	fmt.Printf("\n%s\n  %s\n%s\n\n", line, text, line)
}

func printResult(label string, resp *http.Response, rpc *jsonrpcResponse, err error) {
	if err != nil {
		fmt.Printf("  [ERROR] %s: %v\n", label, err)
		return
	}
	status := "???"
	if resp != nil {
		status = resp.Status
	}
	if rpc != nil && rpc.Error != nil {
		fmt.Printf("  [REJECTED] %s — HTTP %s — code %d: %s\n", label, status, rpc.Error.Code, rpc.Error.Message)
		return
	}
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		fmt.Printf("  [RATE LIMITED] %s — HTTP %s — retry after %ss\n", label, status, retryAfter)
		return
	}
	if resp != nil && resp.StatusCode >= 400 {
		fmt.Printf("  [FAILED] %s — HTTP %s\n", label, status)
		return
	}

	if rpc != nil && rpc.Result != nil {
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, rpc.Result, "    ", "  ")
		fmt.Printf("  [OK] %s — HTTP %s\n    %s\n", label, status, pretty.String())
	} else {
		fmt.Printf("  [OK] %s — HTTP %s\n", label, status)
	}
}

func main() {
	apiKey := os.Getenv("DEMO_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: DEMO_API_KEY environment variable is required.")
		fmt.Fprintln(os.Stderr, "Generate one with:  go run ./cmd/keygen -name demo-agent")
		os.Exit(1)
	}

	client := newMCPClient(apiKey)

	// ── Step 1: Initialize MCP session ──────────────────────────────────

	header("Step 1: Initialize MCP Session")

	resp, rpc, err := client.send("initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "ai-agent-demo",
			"version": "1.0.0",
		},
	}, true)

	printResult("initialize", resp, rpc, err)
	if err != nil || (rpc != nil && rpc.Error != nil) {
		fmt.Fprintln(os.Stderr, "Cannot continue without a session. Is the gateway running?")
		os.Exit(1)
	}

	client.sessionID = resp.Header.Get("Mcp-Session-Id")
	fmt.Printf("  Session ID: %s\n", client.sessionID)

	// Send initialized notification
	resp, _, err = client.send("notifications/initialized", nil, true)
	if err != nil {
		fmt.Printf("  [WARN] initialized notification failed: %v\n", err)
	} else {
		fmt.Printf("  [OK] notifications/initialized — HTTP %s\n", resp.Status)
	}

	// ── Step 2: List available tools ────────────────────────────────────

	header("Step 2: List Available Tools")

	resp, rpc, err = client.send("tools/list", nil, true)
	printResult("tools/list", resp, rpc, err)

	// ── Step 3: Call each tool with realistic parameters ────────────────

	header("Step 3: Call Tools")

	// 3a: echo
	fmt.Println("  --- echo ---")
	resp, rpc, err = client.send("tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"message": "Hello from the AI agent demo!"},
	}, true)
	printResult("echo", resp, rpc, err)

	// 3b: get_time
	fmt.Println("\n  --- get_time ---")
	resp, rpc, err = client.send("tools/call", map[string]any{
		"name":      "get_time",
		"arguments": map[string]any{},
	}, true)
	printResult("get_time", resp, rpc, err)

	// 3c: add
	fmt.Println("\n  --- add ---")
	resp, rpc, err = client.send("tools/call", map[string]any{
		"name":      "add",
		"arguments": map[string]any{"a": 1337, "b": 42},
	}, true)
	printResult("add(1337, 42)", resp, rpc, err)

	// ── Step 4: Rapid requests to trigger rate limiting ─────────────────

	header("Step 4: Rapid Requests (trigger rate limiting)")

	fmt.Println("  Sending 15 rapid requests...")
	fmt.Println()

	limited := 0
	succeeded := 0

	for i := 1; i <= 15; i++ {
		resp, rpc, err = client.send("tools/call", map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": fmt.Sprintf("burst request #%d", i)},
		}, true)

		if err != nil {
			fmt.Printf("  #%02d [ERROR] %v\n", i, err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			fmt.Printf("  #%02d [RATE LIMITED] retry after %ss\n", i, retryAfter)
			limited++
		} else if rpc != nil && rpc.Error != nil {
			fmt.Printf("  #%02d [REJECTED] code %d: %s\n", i, rpc.Error.Code, rpc.Error.Message)
		} else {
			fmt.Printf("  #%02d [OK]\n", i)
			succeeded++
		}
	}

	fmt.Printf("\n  Summary: %d succeeded, %d rate-limited\n", succeeded, limited)

	// ── Step 5: Unauthenticated request ─────────────────────────────────

	header("Step 5: Unauthenticated Request (no API key)")

	noAuthClient := newMCPClient("")
	resp, rpc, err = noAuthClient.send("initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "unauthorized-client",
			"version": "1.0.0",
		},
	}, false)
	printResult("initialize (no auth)", resp, rpc, err)

	// ── Done ────────────────────────────────────────────────────────────

	header("Demo Complete")
	fmt.Println("  The AI agent demo exercised:")
	fmt.Println("    - MCP session initialization with API key auth")
	fmt.Println("    - Tool discovery (tools/list)")
	fmt.Println("    - Tool execution (echo, get_time, add)")
	fmt.Println("    - Rate limit enforcement")
	fmt.Println("    - Auth rejection for unauthenticated requests")
	fmt.Println()
}
