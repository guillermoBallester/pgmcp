```

  _ __   __ _ _ __ ___   ___ _ __
 | '_ \ / _` | '_ ` _ \ / __| '_ \
 | |_) | (_| | | | | | | (__| |_) |
 | .__/ \__, |_| |_| |_|\___| .__/
 |_|    |___/                |_|

 PostgreSQL ← MCP → LLM
```

[![CI](https://github.com/guillermoBallester/pgmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/guillermoBallester/pgmcp/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![MCP](https://img.shields.io/badge/MCP-2025--03--26-blueviolet)](https://modelcontextprotocol.io)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

A generic MCP server that connects to **any** PostgreSQL database and lets LLMs explore schemas, discover relationships, and run queries — without the user writing any SQL.

**One binary. Any database. No custom code per schema.**

## How It Works

```
┌────────────┐       stdio (JSON-RPC)       ┌─────────┐       SQL        ┌────────────┐
│            │  ──── list_tables ──────────► │         │ ───────────────► │            │
│   Claude   │  ──── describe_table ──────► │  pgmcp  │ ◄─────────────── │ PostgreSQL │
│  (or any   │  ──── query ───────────────► │         │   schema info    │  (any DB)  │
│  MCP LLM)  │  ◄──── JSON results ──────── │         │   + query rows   │            │
└────────────┘                               └─────────┘                  └────────────┘
```

1. Point `pgmcp` at a PostgreSQL database (just a connection string)
2. An LLM connects via MCP protocol
3. The LLM autonomously calls `list_tables` → `describe_table` → `query` to discover the schema and answer questions
4. The user gets answers in natural language

The LLM is the intelligence layer. The server is the safe bridge between the model and the database.

## Quick Start

### Build from source

```bash
make build-all    # builds bin/pgmcp-server and bin/pgmcp-agent
```

### Run the tunnel (server + agent)

pgmcp uses a tunnel architecture: the **server** exposes MCP over HTTP, the **agent** runs next to the database and connects outbound to the server.

```bash
# Terminal 1 — start the server
make run-server

# Terminal 2 — start the agent (connects to RNAcentral public DB by default)
make run-agent
```

Or run both in a single terminal:

```bash
make run-tunnel
```

Verify it's working:

```bash
curl http://localhost:8080/health   # → {"status":"ok"} (server is alive)
curl http://localhost:8080/ready    # → {"status":"ready"} (agent connected)
```

To point at your own database:

```bash
make run-agent DATABASE_URL="postgres://user:pass@host:5432/mydb"
```

### Claude Desktop integration

Claude Desktop speaks MCP over stdio, so you need a bridge to connect it to the server's HTTP endpoint. Use [`@pyroprompts/mcp-stdio-to-streamable-http-adapter`](https://www.npmjs.com/package/@pyroprompts/mcp-stdio-to-streamable-http-adapter) (requires Node.js).

Add this to your `claude_desktop_config.json` (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "pgmcp": {
      "command": "npx",
      "args": [
        "@pyroprompts/mcp-stdio-to-streamable-http-adapter"
      ],
      "env": {
        "URI": "http://localhost:8080/mcp",
        "MCP_NAME": "pgmcp"
      }
    }
  }
}
```

Make sure the server and agent are running (`make run-server` + `make run-agent`) before restarting Claude Desktop.

### Try it with the demo

A self-contained demo with a seeded e-commerce database:

```bash
make demo-up
```

This starts a Postgres container with sample data (customers, products, orders) and a `pgmcp` instance connected to it.

```bash
make demo-down   # tear it down
```

### Try it with a public database

The default `make run-agent` connects to [RNAcentral](https://rnacentral.org/help/public-database), a public genomics database hosted by the European Bioinformatics Institute:

```bash
make run-server   # Terminal 1
make run-agent    # Terminal 2 — connects to RNAcentral by default
```

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `READ_ONLY` | `true` | Wrap queries in read-only transactions |
| `MAX_ROWS` | `100` | Server-side row limit enforced on all queries |
| `QUERY_TIMEOUT` | `10s` | Per-query execution timeout |
| `SCHEMAS` | *(all)* | Comma-separated list of schemas to expose (e.g. `public,app`) |

When `SCHEMAS` is not set, all non-system schemas are exposed. When set, only the listed schemas are visible — both for listing tables and describing them. This acts as an allowlist.

## MCP Tools

### `list_schemas`

Lists all available database schemas. Call this first to discover what schemas exist.

**Parameters:** none

### `list_tables`

Lists all tables in the database with their schema, estimated row count, and comments.

**Parameters:** none

### `describe_table`

Returns detailed metadata for a table: columns (name, type, nullable, default, comment), primary keys, foreign keys, and indexes.

**Parameters:**
- `table_name` (string, required)
- `schema` (string, optional — resolves automatically if omitted)

### `query`

Executes a SQL query and returns results as a JSON array of objects.

**Parameters:**
- `sql` (string, required)

**Safety:** Queries run inside a transaction with the configured access mode (read-only by default), a server-side row limit (the query is wrapped in `SELECT * FROM (...) LIMIT N`), and a context timeout.

## Architecture

Hexagonal architecture. Single binary, synchronous request-response over stdio.

```
pgmcp/
│
├── cmd/
│   └── pg-mcp/
│       └── main.go                  # Entrypoint: config → pool → adapters → MCP server → stdio
│
├── pkg/
│   ├── core/
│   │   ├── ports/
│   │   │   ├── explorer.go          # SchemaExplorer interface + DTOs (TableInfo, ColumnInfo, etc.)
│   │   │   └── executor.go          # QueryExecutor interface
│   │   ├── domain/
│   │   │   └── validator.go         # SQL validation: whitelist SELECT/EXPLAIN, reject DDL/DML
│   │   └── service/
│   │       ├── explorer_service.go  # Schema exploration orchestration
│   │       └── query_service.go     # Query validation + execution orchestration
│   └── app/
│       ├── server.go                # MCP server factory — wires tools to port implementations
│       └── tools.go                 # Tool definitions, descriptions, and JSON-RPC handlers
│
├── internal/
│   ├── adapter/
│   │   └── postgres/
│   │       ├── pool.go              # pgxpool initialization + health check
│   │       ├── queries.go           # SQL constants (information_schema, pg_catalog)
│   │       ├── explorer.go          # SchemaExplorer impl — schema-filtered table discovery
│   │       ├── fetchers.go          # Private helpers: columns, PKs, FKs, indexes
│   │       ├── executor.go          # QueryExecutor impl — read-only tx, row limit, timeout
│   │       └── explorer_test.go     # Integration tests (testcontainers + testify)
│   └── config/
│       └── config.go                # Config struct, env var loading, validation
│
├── examples/demo/
│   ├── docker-compose.yml           # Self-contained demo: Postgres + pgmcp + seed data
│   └── seed.sql                     # E-commerce schema (customers, products, orders)
│
├── .github/workflows/
│   ├── ci.yml                       # Build → Test → Lint → Docker
│   └── security.yml                 # Trivy + govulncheck + CodeQL
│
├── Dockerfile                       # Multi-stage: golang:1.26-alpine → alpine:3.21
├── docker-compose.yml               # Production: pg-mcp only, bring your own DATABASE_URL
├── Makefile
└── go.mod
```

### Data flow

```
main.go
  │
  ├── config.Load()           ← reads env vars (DATABASE_URL, READ_ONLY, etc.)
  ├── postgres.NewPool()      ← creates pgxpool, pings DB
  ├── postgres.NewExplorer()  ← implements SchemaExplorer (queries information_schema)
  ├── postgres.NewExecutor()  ← implements QueryExecutor (read-only tx, row limit)
  ├── app.NewServer()         ← creates MCP server, registers tools
  └── server.ServeStdio()     ← blocks, reads JSON-RPC from stdin, writes to stdout
```

### Key design decisions

- **Ports & adapters** — `SchemaExplorer` and `QueryExecutor` are interfaces. The Postgres adapter implements them. This keeps the MCP layer decoupled from the database driver and makes testing straightforward.
- **No domain entities** — this server works with dynamic schemas discovered at runtime, not fixed models.
- **No SQLC** — queries against system catalogs are inherently dynamic. Using pgx directly is simpler.
- **No AI/LLM dependencies** — the server has zero knowledge of which LLM is calling it. It's a pure MCP tool provider.
- **Schema filtering** — the `SCHEMAS` env var acts as an allowlist, restricting what the LLM can see and query. Useful for databases with internal schemas that shouldn't be exposed.
- **`pkg/` for shared, `internal/` for private** — packages in `pkg/` (ports, domain, services, MCP wiring) are importable by external modules. The Postgres adapter and SaaS platform code stay in `internal/` because they're private to this module. Go's `internal/` convention enforces the same privacy boundary that a separate repo would.

### Safety layer

Since an LLM is generating SQL, guardrails are essential:

1. **Read-only transactions** (default) — queries run inside `SET TRANSACTION READ ONLY`
2. **Row limit** — server-side `LIMIT` injection, independent of what the LLM generates
3. **Query timeout** — `context.WithTimeout` on every execution
4. **Schema filtering** — restrict visibility to specific schemas
5. **Connection pooling** — pgxpool with bounded connections to prevent resource exhaustion

### Query Policies (Coming Soon)

The current safety layer covers the basics. Query policies take governance further — they're the feature that turns a CTO's "no" into "yes" when teams want AI access to production databases.

Planned policy capabilities:

- **Table allow/deny lists** — restrict which tables the LLM can see and query
- **Column masking** — mask or hide sensitive columns (e.g., emails, SSNs) in query results
- **Row filtering** — automatically inject row-level filters (e.g., only recent data)
- **Rate limiting** — cap queries per hour, max rows per query, block full table scans

Example policy configuration (planned):

```yaml
policy:
  allow_tables:
    - orders
    - products
    - categories
  deny_tables:
    - users
    - payments
  tables:
    customers:
      allow_columns: [id, name, city, created_at]
      mask_columns:
        email: "****@****.com"
      deny_columns: [ssn, tax_id]
    orders:
      row_filter: "created_at > NOW() - INTERVAL '90 days'"
  max_queries_per_hour: 100
  max_rows_per_query: 50
```

Policies are evaluated at two layers for defense in depth: the cloud server enforces identity-based policies (user X can only access tables Y, Z), while the agent enforces SQL-level policies (no DDL, row limit, timeout).

### Audit Trail (Coming Soon)

pgmcp sits between the LLM and the database — it sees both the MCP context (user, tool, session) and the SQL context (query, results, timing). This unique position enables audit capabilities that Postgres logs alone cannot provide.

Every MCP call will be logged with:

- **Timestamp** and **duration**
- **User identity** (who triggered the query)
- **MCP tool** invoked (`list_tables`, `describe_table`, `query`)
- **SQL executed** and **rows returned**
- **Policy evaluation results** (passed, blocked, warnings)
- **Session and agent IDs** for full traceability

Why this matters:

- **Compliance** — HIPAA, SOC2, and PCI-DSS require logging access to sensitive data. Without audit trails, AI database access is shadow IT. With them, it's compliant.
- **Incident response** — when a customer reports a data concern, the security team can search exactly what queries ran, who ran them, and what data was returned.
- **Usage visibility** — CTOs can see whether AI database access is delivering value: which databases, how many queries, which teams.

## Development

### Prerequisites

- Go 1.26+
- Docker (for tests and demo)

### Commands

```bash
make build        # Build the binary
make test         # Run all tests (requires Docker for testcontainers)
make test-short   # Run tests without integration tests
make lint         # Run golangci-lint
make fmt          # Format code
make vet          # Run go vet
make tidy         # Tidy go.mod

make demo-up      # Start the demo (Postgres + pgmcp + seed data)
make demo-down    # Tear down the demo

make docker-build # Build the Docker image
make clean        # Remove binary + demo containers
```

### Testing

Tests use [testcontainers-go](https://golang.testcontainers.org/) to spin up real PostgreSQL instances. No mocks — every test runs against an actual database.

```bash
make test
```

### CI

GitHub Actions runs on every push and PR:

1. **Build** — compile + `go vet`
2. **Test** — all tests with `-race`
3. **Lint** — golangci-lint
4. **Docker** — verify the image builds

## Docker

### Run against your own database

```bash
DATABASE_URL="postgres://user:pass@host:5432/mydb" docker compose up
```

### Build the image

```bash
docker build -t pgmcp .
```

The image is a multi-stage build: compiles with `golang:1.26-alpine`, runs on `alpine:3.21` (~14MB binary).

## SaaS Mode (Coming Soon)

pgmcp is evolving into a SaaS platform that lets LLMs query private, on-premise databases without VPNs or inbound firewall rules. All SaaS components live in this repo — the agent binary under `cmd/pgmcp-agent/`, shared tunnel types under `pkg/tunnel/`, and the cloud server under `internal/saas/` (private to this module).

### How it works

1. You deploy a lightweight **bridge agent** (`pgmcp-agent`) next to your database
2. The agent establishes an **outbound** WebSocket connection to the pgmcp cloud server
3. Claude (or any MCP client) connects to the cloud server
4. Queries flow through the tunnel — your database credentials never leave your infrastructure

```
┌──────────────┐   SSE/HTTP (MCP)    ┌──────────────────┐  yamux stream   ┌───────────────┐
│              │ ──── list_tables ──► │                  │ ──────────────► │               │
│  Claude /    │ ──── describe   ──► │  pgmcp cloud     │                 │  pgmcp-agent  │
│  any MCP     │ ──── query      ──► │                  │ ◄────────────── │  (on-prem)    │
│  client      │ ◄── JSON results ── │                  │  JSON response  │               │
└──────────────┘                     └──────────────────┘                  └───────┬───────┘
                                                            ▲                      │
                                                            │ outbound WS          │ local
                                                            │ (port 443)           │ pgxpool
                                                            │                      ▼
                                                         No inbound        ┌──────────────┐
                                                         ports needed      │  Your DB     │
                                                                           └──────────────┘
```

### Security

- **Zero-trust:** The cloud server never stores or sees database credentials
- **Outbound only:** The agent initiates the connection — no open ports, no VPN
- **Defense-in-depth:** SQL validation on both server and agent (9 security layers)
- **Open source agent:** Every line of code running on your infrastructure is auditable

## Roadmap

### Standalone improvements

- [ ] `list_schemas` tool — help LLMs navigate multi-schema databases
- [ ] Schema-qualified table names — accept `schema.table` in `describe_table`
- [ ] `explain` tool — expose `EXPLAIN ANALYZE` for query plan inspection
- [ ] GoReleaser + GitHub Releases — prebuilt binaries for all platforms
- [ ] NPX wrapper — `npx pgmcp` for zero-install usage from Claude Desktop
- [ ] pgvector support — semantic search tool if pgvector extension is detected
- [ ] Multi-database — connect to multiple databases from one server instance

### SaaS platform (build order)

- [ ] **Tunnel MVP** — WebSocket agent, cloud proxy, basic API-key auth
- [ ] **Audit log v1** — log every MCP call with user/session/SQL context
- [ ] **Table-level policies** — allow/deny lists for tables, configured per agent
- [ ] **MySQL adapter** — second database backend, validate "any database" positioning
- [ ] **Dashboard** — audit log viewer, agent status, usage stats
- [ ] **Column masking** — mask/hide sensitive columns in query results

## Version Tagging

Releases follow [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`.

- Tags are created via [GitHub Releases](https://docs.github.com/en/repositories/releasing-projects-on-github/managing-releases-in-a-repository)
- The binary version is injected at build time via `-ldflags "-X main.version=v1.2.3"`
- After the repo split, agent and server versions will be tracked independently

## License

[Apache 2.0](LICENSE)
