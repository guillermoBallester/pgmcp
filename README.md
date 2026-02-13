# pgmcp

A generic MCP server that connects to **any** PostgreSQL database and lets LLMs explore schemas, discover relationships, and run queries — without the user writing any SQL.

One binary. Any database. No custom code per schema.

## How It Works

1. Point `pgmcp` at a PostgreSQL database (just a connection string)
2. An LLM (Claude, GPT, etc.) connects via MCP protocol
3. The LLM autonomously calls `list_tables` -> `describe_table` -> `query` to discover the schema and answer questions
4. The user gets answers in natural language

The LLM is the intelligence layer. The server is the safe bridge between the model and the database.

## Quick Start

### Build from source

```bash
go build -o pg-mcp ./cmd/pg-mcp
```

### Run

```bash
DATABASE_URL="postgres://user:pass@localhost:5432/mydb" ./pg-mcp
```

The server communicates over **stdio** (JSON-RPC), which is how MCP clients like Claude Desktop connect to it.

### Claude Desktop integration

Add this to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "my-database": {
      "command": "/path/to/pg-mcp",
      "env": {
        "DATABASE_URL": "postgres://readonly:pass@host:5432/mydb",
        "READ_ONLY": "true",
        "MAX_ROWS": "50"
      }
    }
  }
}
```

You can run multiple instances pointing at different databases — same binary, different env vars.

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

You can test against [RNAcentral](https://rnacentral.org/help/public-database), a public genomics database hosted by the European Bioinformatics Institute:

```bash
DATABASE_URL="postgres://reader:NWDMCE5xdipIjRrp@hh-pgsql-public.ebi.ac.uk:5432/pfmegrnargs" ./pg-mcp
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

### `list_tables`

Lists all tables in the database with their schema, estimated row count, and comments.

**Parameters:** none

### `describe_table`

Returns detailed metadata for a table: columns (name, type, nullable, default, comment), primary keys, foreign keys, and indexes.

**Parameters:**
- `table_name` (string, required)

### `query`

Executes a SQL query and returns results as a JSON array of objects.

**Parameters:**
- `sql` (string, required)

**Safety:** Queries run inside a transaction with the configured access mode (read-only by default), a server-side row limit (the query is wrapped in `SELECT * FROM (...) LIMIT N`), and a context timeout.

## Architecture

Hexagonal architecture. Single binary, synchronous request-response over stdio.

```
cmd/
  pg-mcp/
    main.go                        # Entrypoint: config -> pool -> adapters -> MCP server

internal/
  config/
    config.go                      # Config struct, env loading, validation

  core/ports/
    explorer.go                    # SchemaExplorer interface + DTOs
    executor.go                    # QueryExecutor interface

  adapter/postgres/
    pool.go                        # pgxpool initialization + ping
    explorer.go                    # SchemaExplorer implementation
    fetchers.go                    # Private fetcher methods (columns, PKs, FKs, indexes)
    queries.go                     # SQL query constants
    executor.go                    # QueryExecutor implementation (read-only tx, row limit, timeout)
    explorer_test.go               # Integration tests (testcontainers)

  app/
    server.go                      # MCP server factory
    tools.go                       # Tool definitions + handlers

examples/
  demo/
    docker-compose.yml             # Postgres + pgmcp wired together
    seed.sql                       # Sample e-commerce schema + data
```

### Key design decisions

- **Ports & adapters** — `SchemaExplorer` and `QueryExecutor` are interfaces. The Postgres adapter implements them. This keeps the MCP layer decoupled from the database driver and makes testing straightforward.
- **No domain entities** — this server works with dynamic schemas discovered at runtime, not fixed models.
- **No SQLC** — queries against system catalogs are inherently dynamic. Using pgx directly is simpler.
- **No AI/LLM dependencies** — the server has zero knowledge of which LLM is calling it. It's a pure MCP tool provider.
- **Schema filtering** — the `SCHEMAS` env var acts as an allowlist, restricting what the LLM can see and query. Useful for databases with internal schemas that shouldn't be exposed.

### Safety layer

Since an LLM is generating SQL, guardrails are essential:

1. **Read-only transactions** (default) — queries run inside `SET TRANSACTION READ ONLY`
2. **Row limit** — server-side `LIMIT` injection, independent of what the LLM generates
3. **Query timeout** — `context.WithTimeout` on every execution
4. **Schema filtering** — restrict visibility to specific schemas
5. **Connection pooling** — pgxpool with bounded connections to prevent resource exhaustion

## Development

### Prerequisites

- Go 1.25+
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

The image is a multi-stage build: compiles with `golang:1.25-alpine`, runs on `alpine:3.21` (~14MB binary).

## Roadmap

- [ ] `list_schemas` tool — help LLMs navigate multi-schema databases
- [ ] Schema-qualified table names — accept `schema.table` in `describe_table`
- [ ] Logging to stderr — structured logging for debugging (stdout is reserved for MCP protocol)
- [ ] Graceful shutdown — signal handling for clean connection teardown
- [ ] Statement validation — parse SQL and reject DDL/DML in read-only mode before sending to Postgres
- [ ] `explain` tool — expose `EXPLAIN ANALYZE` for query plan inspection
- [ ] GoReleaser + GitHub Releases — prebuilt binaries for all platforms
- [ ] NPX wrapper — `npx pgmcp` for zero-install usage from Claude Desktop
- [ ] pgvector support — semantic search tool if pgvector extension is detected
- [ ] Write mode — `insert`, `update`, `delete` tools with confirmation prompts
- [ ] Multi-database — connect to multiple databases from one server instance

## License

MIT
