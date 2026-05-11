# Architecture

## Overview

The MCP Gateway is a reverse proxy written in Go. It sits between AI agents and MCP servers, adding security and observability without changing the MCP protocol.

## Request flow

1. An AI agent sends an MCP request to the gateway on port 8080
2. The gateway authenticates the request using the API key in the Authorization header
3. It checks whether the API key has permission to call the requested tool
4. If allowed, the request is forwarded to the upstream MCP server
5. The response comes back through the gateway
6. The entire interaction is logged to PostgreSQL for audit purposes
7. The response is returned to the AI agent

## Components

The project follows standard Go project layout with the main packages under internal/:

- proxy: the core reverse proxy using net/http/httputil
- auth: API key generation, hashing, and validation middleware
- audit: async logging of all requests and responses
- ratelimit: token bucket rate limiting per API key
- rbac: role and permission checking
- config: environment variable configuration
- database: PostgreSQL connection management
- store: data access layer

## Key decisions

I chose Go because it's the industry standard for gateway and proxy services. The standard library's net/http and httputil packages handle most of the heavy lifting.

PostgreSQL was chosen over SQLite because enterprise customers expect it and it handles concurrent writes well, which matters for audit logging under load.

sqlc generates Go code from SQL queries, which gives type safety without the abstraction overhead of an ORM.

The audit logger uses a buffered channel and background goroutine so that database writes don't slow down request handling.

## Database

Six tables store all gateway state:

- api_keys: hashed API keys with expiry and revocation support
- roles: named roles like "readonly" or "admin"
- permissions: tool-level permissions attached to roles
- api_key_roles: maps keys to roles
- rate_limits: per-key rate limit configuration
- audit_logs: complete record of every request