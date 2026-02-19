package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/guillermoBallester/isthmus/internal/adapter/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const testSchema = `
	CREATE TABLE customers (
		id   SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE
	);
	COMMENT ON TABLE customers IS 'Main customer table';
	COMMENT ON COLUMN customers.name IS 'Full name';

	CREATE TABLE orders (
		id          SERIAL PRIMARY KEY,
		customer_id INTEGER NOT NULL REFERENCES customers(id),
		total       NUMERIC(10,2) NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX idx_orders_customer ON orders(customer_id);
	COMMENT ON TABLE orders IS 'Customer orders';

	CREATE VIEW customer_emails AS SELECT id, email FROM customers;
	COMMENT ON VIEW customer_emails IS 'Customer email addresses';
`

const testSchemaMulti = `
	CREATE SCHEMA app;
	CREATE SCHEMA internal;

	CREATE TABLE app.users (
		id   SERIAL PRIMARY KEY,
		name TEXT NOT NULL
	);
	COMMENT ON TABLE app.users IS 'App users';

	CREATE TABLE internal.jobs (
		id     SERIAL PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'pending'
	);

	CREATE TABLE public.config (
		key   TEXT PRIMARY KEY,
		value TEXT
	);
`

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	_, err = pool.Exec(ctx, testSchema)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE")
	require.NoError(t, err)

	return pool
}

func setupMultiSchemaDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	_, err = pool.Exec(ctx, testSchemaMulti)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE")
	require.NoError(t, err)

	return pool
}

func TestListSchemas(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	schemas, err := explorer.ListSchemas(ctx)
	require.NoError(t, err)

	names := make([]string, len(schemas))
	for i, s := range schemas {
		names[i] = s.Name
	}
	assert.Contains(t, names, "public")
	assert.NotContains(t, names, "pg_catalog")
	assert.NotContains(t, names, "information_schema")
}

func TestListSchemas_SchemaFilter(t *testing.T) {
	pool := setupMultiSchemaDB(t)
	ctx := context.Background()

	t.Run("filtered to app", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, []string{"app"})
		schemas, err := explorer.ListSchemas(ctx)
		require.NoError(t, err)
		require.Len(t, schemas, 1)
		assert.Equal(t, "app", schemas[0].Name)
	})

	t.Run("no filter shows all non-system schemas", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, nil)
		schemas, err := explorer.ListSchemas(ctx)
		require.NoError(t, err)

		names := make([]string, len(schemas))
		for i, s := range schemas {
			names[i] = s.Name
		}
		assert.Contains(t, names, "app")
		assert.Contains(t, names, "internal")
		assert.Contains(t, names, "public")
	})
}

func TestListTables(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	tables, err := explorer.ListTables(ctx)
	require.NoError(t, err)

	assert.Len(t, tables, 3)

	type tableInfo struct {
		comment string
		typ     string
	}
	tableMap := make(map[string]tableInfo)
	for _, tbl := range tables {
		tableMap[tbl.Name] = tableInfo{comment: tbl.Comment, typ: tbl.Type}
		assert.Equal(t, "public", tbl.Schema)
	}

	assert.Equal(t, "Main customer table", tableMap["customers"].comment)
	assert.Equal(t, "table", tableMap["customers"].typ)
	assert.Equal(t, "Customer orders", tableMap["orders"].comment)
	assert.Equal(t, "table", tableMap["orders"].typ)
	assert.Equal(t, "Customer email addresses", tableMap["customer_emails"].comment)
	assert.Equal(t, "view", tableMap["customer_emails"].typ)
}

func TestDescribeTable_Columns(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "", "customers")
	require.NoError(t, err)

	assert.Equal(t, "public", detail.Schema)
	assert.Equal(t, "customers", detail.Name)
	assert.Equal(t, "Main customer table", detail.Comment)

	require.Len(t, detail.Columns, 3)

	id := detail.Columns[0]
	assert.Equal(t, "id", id.Name)
	assert.Equal(t, "integer", id.DataType)
	assert.True(t, id.IsPrimaryKey)
	assert.False(t, id.IsNullable)

	name := detail.Columns[1]
	assert.Equal(t, "name", name.Name)
	assert.Equal(t, "text", name.DataType)
	assert.False(t, name.IsNullable)
	assert.Equal(t, "Full name", name.Comment)

	email := detail.Columns[2]
	assert.Equal(t, "email", email.Name)
	assert.True(t, email.IsNullable)
}

func TestDescribeTable_ForeignKeys(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "", "orders")
	require.NoError(t, err)

	require.Len(t, detail.ForeignKeys, 1)
	fk := detail.ForeignKeys[0]
	assert.Equal(t, "customer_id", fk.ColumnName)
	assert.Equal(t, "customers", fk.ReferencedTable)
	assert.Equal(t, "id", fk.ReferencedColumn)
}

func TestDescribeTable_Indexes(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "", "orders")
	require.NoError(t, err)

	indexNames := make(map[string]bool)
	for _, idx := range detail.Indexes {
		indexNames[idx.Name] = idx.IsUnique
	}

	assert.True(t, indexNames["orders_pkey"])
	assert.False(t, indexNames["idx_orders_customer"])
}

func TestDescribeTable_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	_, err := explorer.DescribeTable(ctx, "", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestListTables_SchemaFilter(t *testing.T) {
	pool := setupMultiSchemaDB(t)
	ctx := context.Background()

	t.Run("single schema", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, []string{"app"})
		tables, err := explorer.ListTables(ctx)
		require.NoError(t, err)

		require.Len(t, tables, 1)
		assert.Equal(t, "app", tables[0].Schema)
		assert.Equal(t, "users", tables[0].Name)
	})

	t.Run("multiple schemas", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, []string{"app", "public"})
		tables, err := explorer.ListTables(ctx)
		require.NoError(t, err)

		assert.Len(t, tables, 2)
		schemas := map[string]bool{}
		for _, tbl := range tables {
			schemas[tbl.Schema] = true
		}
		assert.True(t, schemas["app"])
		assert.True(t, schemas["public"])
		assert.False(t, schemas["internal"])
	})

	t.Run("nonexistent schema returns empty", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, []string{"doesnotexist"})
		tables, err := explorer.ListTables(ctx)
		require.NoError(t, err)
		assert.Empty(t, tables)
	})

	t.Run("no filter shows all non-system schemas", func(t *testing.T) {
		explorer := postgres.NewExplorer(pool, nil)
		tables, err := explorer.ListTables(ctx)
		require.NoError(t, err)

		assert.Len(t, tables, 3)
	})
}

func TestDescribeTable_WithSchema(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "public", "customers")
	require.NoError(t, err)
	assert.Equal(t, "public", detail.Schema)
	assert.Equal(t, "customers", detail.Name)
	assert.Equal(t, "Main customer table", detail.Comment)
	assert.NotEmpty(t, detail.Columns)
}

func TestDescribeTable_WithSchema_MultiSchema(t *testing.T) {
	pool := setupMultiSchemaDB(t)
	explorer := postgres.NewExplorer(pool, nil)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "app", "users")
	require.NoError(t, err)
	assert.Equal(t, "app", detail.Schema)
	assert.Equal(t, "users", detail.Name)
	assert.Equal(t, "App users", detail.Comment)
}

func TestDescribeTable_SchemaFilter_BlocksAccess(t *testing.T) {
	pool := setupMultiSchemaDB(t)
	ctx := context.Background()

	explorer := postgres.NewExplorer(pool, []string{"app"})

	// Table exists in 'public' but explorer is restricted to 'app'
	_, err := explorer.DescribeTable(ctx, "", "config")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config")
	assert.Contains(t, err.Error(), "app")
}
