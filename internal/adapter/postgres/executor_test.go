package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/guillermoBallester/isthmus/internal/adapter/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecute_Explain(t *testing.T) {
	pool := setupTestDB(t)
	executor := postgres.NewExecutor(pool, true, 100, 10*time.Second)
	ctx := context.Background()

	results, err := executor.Execute(ctx, "EXPLAIN SELECT * FROM customers")
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestExecute_Select_RowLimit(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()

	// Insert 5 rows
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(ctx, "INSERT INTO customers (name, email) VALUES ($1, $2)",
			"user", nil)
		require.NoError(t, err)
	}

	executor := postgres.NewExecutor(pool, true, 3, 10*time.Second)

	results, err := executor.Execute(ctx, "SELECT id, name FROM customers")
	require.NoError(t, err)
	assert.Len(t, results, 3, "should be limited to maxRows=3")
}
