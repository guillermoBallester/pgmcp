package postgres

import (
	"context"
	"fmt"

	"github.com/guillermoballestersasso/pgmcp/internal/core/ports"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Explorer struct {
	pool *pgxpool.Pool
}

func NewExplorer(pool *pgxpool.Pool) *Explorer {
	return &Explorer{pool: pool}
}

func (e *Explorer) ListTables(ctx context.Context) ([]ports.TableInfo, error) {
	rows, err := e.pool.Query(ctx, queryListTables)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tables []ports.TableInfo
	for rows.Next() {
		var t ports.TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowEstimate, &t.Comment); err != nil {
			return nil, fmt.Errorf("scanning table row: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (e *Explorer) DescribeTable(ctx context.Context, tableName string) (*ports.TableDetail, error) {
	detail := &ports.TableDetail{Name: tableName}

	var err error
	detail.Schema, detail.Comment, err = e.fetchTableMeta(ctx, tableName)
	if err != nil {
		return nil, err
	}

	detail.Columns, err = e.fetchColumns(ctx, detail.Schema, tableName)
	if err != nil {
		return nil, err
	}

	if err := e.markPrimaryKeys(ctx, detail); err != nil {
		return nil, err
	}

	detail.ForeignKeys, err = e.fetchForeignKeys(ctx, detail.Schema, tableName)
	if err != nil {
		return nil, err
	}

	detail.Indexes, err = e.fetchIndexes(ctx, detail.Schema, tableName)
	if err != nil {
		return nil, err
	}

	return detail, nil
}
