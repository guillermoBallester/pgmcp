package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Explorer struct {
	pool    *pgxpool.Pool
	schemas []string // empty means all non-system schemas
}

func NewExplorer(pool *pgxpool.Pool, schemas []string) *Explorer {
	return &Explorer{pool: pool, schemas: schemas}
}

// schemaFilter returns a SQL WHERE clause fragment and args for filtering by schema.
// paramOffset is the starting $N parameter index (1-based).
func (e *Explorer) schemaFilter(column string, paramOffset int) (clause string, args []any) {
	if len(e.schemas) == 0 {
		return fmt.Sprintf("%s NOT IN ('pg_catalog', 'information_schema')", column), nil
	}
	placeholders := make([]string, len(e.schemas))
	args = make([]any, len(e.schemas))
	for i, s := range e.schemas {
		placeholders[i] = fmt.Sprintf("$%d", paramOffset+i)
		args[i] = s
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), args
}

func (e *Explorer) ListSchemas(ctx context.Context) ([]port.SchemaInfo, error) {
	filter, args := e.schemaFilter("s.schema_name", 1)
	query := fmt.Sprintf(queryListSchemas, filter)

	rows, err := e.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing schemas: %w", err)
	}
	defer rows.Close()

	var schemas []port.SchemaInfo
	for rows.Next() {
		var s port.SchemaInfo
		if err := rows.Scan(&s.Name); err != nil {
			return nil, fmt.Errorf("scanning schema row: %w", err)
		}
		schemas = append(schemas, s)
	}
	return schemas, rows.Err()
}

func (e *Explorer) ListTables(ctx context.Context) ([]port.TableInfo, error) {
	filter, args := e.schemaFilter("t.table_schema", 1)
	query := fmt.Sprintf(queryListTables, filter)

	rows, err := e.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tables []port.TableInfo
	for rows.Next() {
		var t port.TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.Type, &t.RowEstimate, &t.Comment); err != nil {
			return nil, fmt.Errorf("scanning table row: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (e *Explorer) DescribeTable(ctx context.Context, schema, tableName string) (*port.TableDetail, error) {
	detail := &port.TableDetail{Name: tableName}

	var err error
	if schema != "" {
		detail.Schema = schema
		detail.Comment, err = e.fetchTableComment(ctx, schema, tableName)
	} else {
		detail.Schema, detail.Comment, err = e.fetchTableMeta(ctx, tableName)
	}
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
