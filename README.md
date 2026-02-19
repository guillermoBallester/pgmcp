```
  _     _   _
 (_)___| |_| |__  _ __ ___  _   _ ___
 | / __| __| '_ \| '_ ` _ \| | | / __|
 | \__ \ |_| | | | | | | | | |_| \__ \
 |_|___/\__|_| |_|_| |_| |_|\__,_|___/

 Database ← MCP → LLM
```

[![CI](https://github.com/guillermoBallester/isthmus/actions/workflows/ci.yml/badge.svg)](https://github.com/guillermoBallester/isthmus/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![MCP](https://img.shields.io/badge/MCP-2025--03--26-blueviolet)](https://modelcontextprotocol.io)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

**Connect any database to any LLM in 2 minutes.**

No VPN. No open ports. No credentials in the cloud. Zero config per schema.

## How It Works

```mermaid
flowchart LR
    LLM["Claude / any MCP client"]
    Server["Isthmus Cloud Server"]
    Agent["Isthmus Agent<br/>(on-prem)"]
    DB["Your Database"]

    LLM -- "MCP (HTTP/SSE)" --> Server
    Server -- "yamux over WebSocket" --> Agent
    Agent -- "SQL (local)" --> DB

    style LLM fill:#f9f,stroke:#333
    style Server fill:#bbf,stroke:#333
    style Agent fill:#bfb,stroke:#333
    style DB fill:#fbb,stroke:#333
```

1. Deploy the **agent** next to your database — it connects **outbound** to the cloud server
2. An LLM connects to the cloud server via MCP
3. Queries flow through the tunnel — your credentials never leave your infrastructure
4. The LLM calls `list_schemas` → `list_tables` → `describe_table` → `query` to answer questions in natural language

## Quick Start

### 1. Get an API key

```bash
export API_KEY="your-api-key"
```

### 2. Install the agent

```bash
# Build from source
make build-agent    # → bin/isthmus-agent
```

### 3. Run

```bash
# Terminal 1 — start the server
make run-server

# Terminal 2 — start the agent
make run-agent DATABASE_URL="postgres://user:pass@host:5432/mydb"
```

Or run both in one terminal:

```bash
make run-tunnel
```

Verify:

```bash
curl http://localhost:8080/health   # → {"status":"ok"}
curl http://localhost:8080/ready    # → {"status":"ready"} (agent connected)
```

## Demo

A self-contained demo with a seeded e-commerce database (customers, products, orders):

```bash
make demo-up      # starts Postgres + server + agent with seed data
make demo-down    # tear it down
```

## Claude Desktop Integration

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "isthmus": {
      "command": "npx",
      "args": [
        "@pyroprompts/mcp-stdio-to-streamable-http-adapter"
      ],
      "env": {
        "URI": "http://localhost:8080/mcp",
        "MCP_NAME": "isthmus"
      }
    }
  }
}
```

Requires the server and agent to be running (`make run-server` + `make run-agent`).

## MCP Tools

| Tool | Description |
|------|-------------|
| `list_schemas` | List all available database schemas |
| `list_tables` | List tables with schema, type, row count, and comments |
| `describe_table` | Column details, primary keys, foreign keys, indexes |
| `query` | Execute read-only SQL, returns JSON array of objects |

## Configuration

All configuration is via environment variables on the **agent**:

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `TUNNEL_URL` | *(required)* | WebSocket URL of the cloud server |
| `API_KEY` | *(required)* | API key for tunnel authentication |
| `READ_ONLY` | `true` | Wrap queries in read-only transactions |
| `MAX_ROWS` | `100` | Server-side row limit on all queries |
| `QUERY_TIMEOUT` | `10s` | Per-query execution timeout |
| `SCHEMAS` | *(all)* | Comma-separated schema allowlist (e.g. `public,app`) |

## Architecture

```
isthmus/
├── cmd/
│   ├── isthmus-agent/    # Bridge agent binary (deployed on-prem)
│   └── isthmus-server/   # Cloud server binary
├── pkg/
│   ├── core/
│   │   ├── ports/        # Interfaces: SchemaExplorer, QueryExecutor
│   │   ├── domain/       # SQL validation (pg_query AST parsing)
│   │   └── service/      # Application services
│   ├── app/              # MCP server factory + tool definitions
│   └── tunnel/           # Shared tunnel protocol types + agent
├── internal/
│   ├── adapter/postgres/ # Postgres implementation of ports
│   ├── config/           # Environment variable loading
│   ├── server/           # HTTP server (chi routing, middleware)
│   └── tunnel/           # Cloud-side tunnel server + proxy
├── examples/demo/        # Docker Compose demo with seed data
├── Dockerfile.agent
├── Dockerfile.server
├── docker-compose.yml
└── Makefile
```

Hexagonal architecture (ports & adapters). The core business logic is decoupled from infrastructure. `pkg/` packages are the public API. `internal/` packages are private to this module.

## Safety & Security

- **Outbound only** — the agent initiates the connection. No open ports, no VPN required
- **Zero-trust** — the cloud server never stores or sees database credentials
- **Read-only transactions** — queries run inside `SET TRANSACTION READ ONLY` by default
- **Row limits** — server-side `LIMIT` injection, independent of LLM-generated SQL
- **Query timeout** — `context.WithTimeout` on every execution
- **SQL validation** — whitelist: `SELECT` and `EXPLAIN` only. DDL/DML rejected at the AST level
- **Schema filtering** — restrict visibility to specific schemas via allowlist
- **Open-source agent** — every line of code running on your infrastructure is auditable

## Coming Soon

- **Audit logs** — log every MCP call with user, SQL, results, and timing context
- **Table-level policies** — allow/deny lists per agent, configured via YAML
- **Column masking** — mask or hide sensitive columns (emails, SSNs) in query results
- **Dashboard** — audit viewer, agent status, usage statistics
- **MySQL adapter** — second database backend
- **Supabase auth** — managed authentication for the cloud server

## Development

### Prerequisites

- Go 1.26+
- Docker (for tests and demo)

### Commands

```bash
make build-all     # Build both binaries
make test          # Run all tests (requires Docker for testcontainers)
make test-short    # Unit tests only
make lint          # golangci-lint
make vet           # go vet
make fmt           # gofmt
make tidy          # go mod tidy
make docker-build  # Build Docker images
make docker-up     # Start local dev stack
make docker-down   # Tear down local dev stack
make clean         # Remove bin/ + tear down compose stacks
```

### Testing

Tests use [testcontainers-go](https://golang.testcontainers.org/) to spin up real PostgreSQL instances. No mocks.

```bash
go test -race -count=1 ./...          # All tests (needs Docker)
go test -short -race -count=1 ./...   # Unit tests only
```

## Version Tagging

Releases follow [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`. The binary version is injected at build time via `-ldflags "-X main.version=..."`.

## License

[Apache 2.0](LICENSE)
