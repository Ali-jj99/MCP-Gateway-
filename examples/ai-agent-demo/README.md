# AI Agent Demo

A Go script that simulates an AI agent interacting with the MCP gateway. It walks through the full lifecycle: authentication, session initialization, tool discovery, tool calls, rate limiting, and auth rejection.

## Prerequisites

- The MCP gateway running on port 8080
- The simple MCP server running on port 9090 (upstream)
- PostgreSQL running (for auth and rate limiting)
- A valid API key

## Setup

**1. Start PostgreSQL and the upstream MCP server:**

```bash
# From the repo root
docker-compose up -d   # starts PostgreSQL
go run ./examples/simple-mcp-server &
```

**2. Start the gateway:**

```bash
DATABASE_URL="postgres://mcpgateway:mcpgateway@localhost:5432/mcpgateway?sslmode=disable" \
UPSTREAM_URL="http://localhost:9090/mcp" \
PORT=8080 \
go run ./cmd/gateway
```

**3. Generate an API key:**

```bash
DATABASE_URL="postgres://mcpgateway:mcpgateway@localhost:5432/mcpgateway?sslmode=disable" \
go run ./cmd/keygen -name demo-agent
```

Copy the key that starts with `mcpgw_...` from the output.

## Run the demo

```bash
DEMO_API_KEY="mcpgw_your_key_here" go run ./examples/ai-agent-demo
```

## What it does

| Step | Description |
|------|-------------|
| 1 | Initializes an MCP session through the gateway using the API key |
| 2 | Lists all available tools via `tools/list` |
| 3 | Calls each tool — `echo`, `get_time`, `add` — with realistic parameters |
| 4 | Sends 15 rapid requests to trigger the rate limiter (default burst: 10) |
| 5 | Sends a request without an API key to demonstrate auth rejection |

## Expected output

```
======================================
  Step 1: Initialize MCP Session
======================================

  [OK] initialize — HTTP 200 OK
    { "protocolVersion": "2025-11-25", ... }
  Session ID: a1b2c3d4...
  [OK] notifications/initialized — HTTP 202 Accepted

  ...

======================================
  Step 4: Rapid Requests (trigger rate limiting)
======================================

  Sending 15 rapid requests...

  #01 [OK]
  ...
  #11 [RATE LIMITED] retry after 1s
  ...

  Summary: 10 succeeded, 5 rate-limited

======================================
  Step 5: Unauthenticated Request (no API key)
======================================

  [REJECTED] initialize (no auth) — HTTP 401 Unauthorized — code -32001: missing Authorization header
```
