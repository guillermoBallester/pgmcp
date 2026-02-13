package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/guillermoballestersasso/pgmcp/internal/adapter/postgres"
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

	// Force stats update so n_live_tup is populated
	_, err = pool.Exec(ctx, "ANALYZE")
	require.NoError(t, err)

	return pool
}

func TestListTables(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool)
	ctx := context.Background()

	tables, err := explorer.ListTables(ctx)
	require.NoError(t, err)

	assert.Len(t, tables, 2)

	tableNames := make(map[string]string)
	for _, tbl := range tables {
		tableNames[tbl.Name] = tbl.Comment
		assert.Equal(t, "public", tbl.Schema)
	}

	assert.Equal(t, "Main customer table", tableNames["customers"])
	assert.Equal(t, "Customer orders", tableNames["orders"])
}

func TestDescribeTable_Columns(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "customers")
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
	explorer := postgres.NewExplorer(pool)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "orders")
	require.NoError(t, err)

	require.Len(t, detail.ForeignKeys, 1)
	fk := detail.ForeignKeys[0]
	assert.Equal(t, "customer_id", fk.ColumnName)
	assert.Equal(t, "customers", fk.ReferencedTable)
	assert.Equal(t, "id", fk.ReferencedColumn)
}

func TestDescribeTable_Indexes(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool)
	ctx := context.Background()

	detail, err := explorer.DescribeTable(ctx, "orders")
	require.NoError(t, err)

	indexNames := make(map[string]bool)
	for _, idx := range detail.Indexes {
		indexNames[idx.Name] = idx.IsUnique
	}

	// PK index is unique
	assert.True(t, indexNames["orders_pkey"])
	// Custom index is not unique
	assert.False(t, indexNames["idx_orders_customer"])
}

func TestDescribeTable_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	explorer := postgres.NewExplorer(pool)
	ctx := context.Background()

	_, err := explorer.DescribeTable(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}
