# MCP Gateway

An API gateway for Model Context Protocol (MCP) servers. Provides authentication, authorization, rate limiting, and audit logging for upstream MCP services.

## Quick Start

```bash
# Start PostgreSQL
docker compose up postgres -d

# Build and run
export DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable"
make run
```

## Development

```bash
make build       # Build the binary
make test        # Run tests
make lint        # Run linter
make docker-up   # Start everything in Docker
make docker-down # Stop Docker services
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server listen port |
| `DATABASE_URL` | (required) | PostgreSQL connection string |
| `MIGRATIONS_PATH` | `migrations` | Path to SQL migration files |

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for a detailed breakdown of the system design.
