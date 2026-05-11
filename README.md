# MCP Gateway

A gateway that sits between AI agents and MCP servers, providing authentication, audit logging, role-based access control, and rate limiting. Built in Go.

## Why this exists

AI agents are increasingly connecting to real-world systems through MCP (Model Context Protocol). Banks, healthcare providers, and public sector organisations need to deploy these agents but can't do so without proper security controls and audit trails.

This gateway acts as a checkpoint between the AI agent and the MCP server. Every request passes through it, gets authenticated, logged, and checked against permissions before being forwarded.

## How it works


The gateway receives MCP requests, validates the API key, checks permissions, logs everything to PostgreSQL, enforces rate limits, and then forwards the request to the upstream MCP server. Responses flow back the same way.

## Tech stack

- Go (standard library + chi router)
- PostgreSQL for persistence
- Docker for local development
- sqlc for type-safe database queries
- golangci-lint for code quality

## Getting started

Start PostgreSQL:

```bash
docker compose up postgres -d
```

Set the environment variables and run:

```bash
export DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable"
export UPSTREAM_URL="http://localhost:9090/mcp"
make run
```

## Development

```bash
make build        # compile the binary
make test         # run tests
make lint         # check code quality
make docker-up    # start PostgreSQL
make docker-down  # stop PostgreSQL
```

## Project status

- [x] Project scaffolding
- [x] Reference MCP server
- [x] Core reverse proxy
- [ ] API key authentication
- [ ] Audit logging
- [ ] Rate limiting
- [ ] Role-based access control
- [ ] Admin dashboard
- [ ] Production deployment

## License

MIT
