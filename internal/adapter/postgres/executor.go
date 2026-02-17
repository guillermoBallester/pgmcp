package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Executor struct {
	pool         *pgxpool.Pool
	readOnly     bool
	maxRows      int
	queryTimeout time.Duration
}

func NewExecutor(pool *pgxpool.Pool, readOnly bool, maxRows int, queryTimeout time.Duration) *Executor {
	return &Executor{
		pool:         pool,
		readOnly:     readOnly,
		maxRows:      maxRows,
		queryTimeout: queryTimeout,
	}
}

func (e *Executor) Execute(ctx context.Context, sql string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, e.queryTimeout)
	defer cancel()

	// EXPLAIN statements cannot be wrapped in a subquery
	var wrappedSQL string
	if isExplain(sql) {
		wrappedSQL = sql
	} else {
		wrappedSQL = fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", sql, e.maxRows)
	}

	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: e.accessMode(),
	})
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, wrappedSQL)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	var results []map[string]any

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("reading row values: %w", err)
		}

		row := make(map[string]any, len(fieldDescs))
		for i, fd := range fieldDescs {
			row[fd.Name] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return results, nil
}

func isExplain(sql string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "EXPLAIN")
}

func (e *Executor) accessMode() pgx.TxAccessMode {
	if e.readOnly {
		return pgx.ReadOnly
	}
	return pgx.ReadWrite
}
