# AI Agent Demo

I wrote this to simulate an AI agent interacting with the MCP gateway. It walks
through the full lifecycle: authentication, session initialization, tool
discovery, tool calls, rate limiting, and auth rejection.

## Prerequisites

You need the MCP gateway running on port 8080, the simple MCP server running on
port 9090 as the upstream, PostgreSQL running for auth and rate limiting, and a
valid API key.

## Setup

Start PostgreSQL and the upstream MCP server:

```bash
# from the repo root
docker-compose up -d   # starts PostgreSQL
go run ./examples/simple-mcp-server &
```

Start the gateway:

```bash
DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable" \
UPSTREAM_URL="http://localhost:9090/mcp" \
PORT=8080 \
go run ./cmd/gateway
```

Generate an API key:

```bash
DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable" \
go run ./cmd/keygen -name demo-agent
```

Copy the key that starts with `mcpgw_...` from the output.

## Run the demo

```bash
DEMO_API_KEY="mcpgw_your_key_here" go run ./examples/ai-agent-demo
```

## What it does

The demo runs five steps. Step 1 initializes an MCP session through the gateway
using the API key. Step 2 lists all available tools via `tools/list`. Step 3
calls each tool (`echo`, `get_time`, `add`) with realistic parameters. Step 4
sends 15 rapid requests to trigger the rate limiter, since the default burst is
10. Step 5 sends a request without an API key to show auth rejection.

## Expected output

```
======================================
  Step 1: Initialize MCP Session
======================================

  [OK] initialize, HTTP 200 OK
    { "protocolVersion": "2025-11-25", ... }
  Session ID: a1b2c3d4...
  [OK] notifications/initialized, HTTP 202 Accepted

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

  [REJECTED] initialize (no auth), HTTP 401 Unauthorized, code -32001: missing Authorization header
```
