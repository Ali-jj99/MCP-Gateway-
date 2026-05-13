# Examples

## ai-agent-demo

A Go script that simulates a real AI agent interacting with the gateway end-to-end: session init, tool discovery, tool calls, rate limiting, and auth rejection. See [`ai-agent-demo/README.md`](ai-agent-demo/README.md) for setup instructions.

```bash
DEMO_API_KEY="mcpgw_..." go run ./examples/ai-agent-demo
```

## simple-mcp-server

A reference MCP server implementing the [Model Context Protocol](https://modelcontextprotocol.io) over Streamable HTTP transport with JSON-RPC 2.0.

### Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `echo` | Returns the message you send | `message` (string, required) |
| `get_time` | Returns the current server time | none |
| `add` | Adds two numbers | `a`, `b` (number, required) |

### Running

```bash
# From the repo root
go run ./examples/simple-mcp-server

# Or with a custom port
PORT=3000 go run ./examples/simple-mcp-server
```

The server listens on port 9090 by default at `POST /mcp`.

### Usage

**1. Initialize the session:**

```bash
curl -s -D- http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "curl", "version": "1.0"}
    }
  }'
```

Note the `Mcp-Session-Id` header in the response — include it in all subsequent requests.

**2. Send the initialized notification:**

```bash
curl -s http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{"jsonrpc": "2.0", "method": "notifications/initialized"}'
```

**3. List tools:**

```bash
curl -s http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'
```

**4. Call a tool:**

```bash
curl -s http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {"name": "add", "arguments": {"a": 17, "b": 25}}
  }'
```
