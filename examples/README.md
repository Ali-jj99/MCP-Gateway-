# Examples

## ai-agent-demo

I wrote this to simulate a real AI agent talking to the gateway end-to-end.
It walks through session init, tool discovery, tool calls, rate limiting, and
auth rejection. See [`ai-agent-demo/README.md`](ai-agent-demo/README.md) for
setup instructions.

```bash
DEMO_API_KEY="mcpgw_..." go run ./examples/ai-agent-demo
```

## simple-mcp-server

A reference MCP server I built for testing. It implements the
[Model Context Protocol](https://modelcontextprotocol.io) over Streamable HTTP
transport with JSON-RPC 2.0 and exposes three tools: `echo` returns the message
you send, `get_time` returns the current server time, and `add` adds two
numbers.

```bash
# from the repo root
go run ./examples/simple-mcp-server

# or with a custom port
PORT=3000 go run ./examples/simple-mcp-server
```

The server listens on port 9090 by default at `POST /mcp`.

### Usage

Initialize the session first:

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

Note the `Mcp-Session-Id` header in the response. Include it in all subsequent
requests.

Send the initialized notification:

```bash
curl -s http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{"jsonrpc": "2.0", "method": "notifications/initialized"}'
```

List available tools:

```bash
curl -s http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'
```

Call a tool:

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
