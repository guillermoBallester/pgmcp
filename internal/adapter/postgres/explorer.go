package postgres

import (
	"context"
	"fmt"

	"github.com/guillermoballestersasso/pgmcp/internal/core/ports"
	"github.com/jackc/pgx/v5/pgxpool"
)

const queryListTables = `
	SELECT
		t.table_schema,
		t.table_name,
		COALESCE(s.n_live_tup, 0) AS row_estimate,
		COALESCE(pg_catalog.obj_description(
			(quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass, 'pg_class'
		), '') AS comment
	FROM information_schema.tables t
	LEFT JOIN pg_stat_user_tables s
		ON s.schemaname = t.table_schema AND s.relname = t.table_name
	WHERE t.table_schema NOT IN ('pg_catalog', 'information_schema')
		AND t.table_type = 'BASE TABLE'
	ORDER BY t.table_schema, t.table_name`

const queryTableMeta = `
	SELECT t.table_schema,
		   COALESCE(pg_catalog.obj_description(
			   (quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass, 'pg_class'
		   ), '')
	FROM information_schema.tables t
	WHERE t.table_name = $1
		AND t.table_schema NOT IN ('pg_catalog', 'information_schema')
	LIMIT 1`

const queryColumns = `
	SELECT
		c.column_name,
		c.data_type,
		c.is_nullable = 'YES',
		COALESCE(c.column_default, ''),
		COALESCE(pg_catalog.col_description(
			(quote_ident(c.table_schema) || '.' || quote_ident(c.table_name))::regclass,
			c.ordinal_position
		), '')
	FROM information_schema.columns c
	WHERE c.table_schema = $1 AND c.table_name = $2
	ORDER BY c.ordinal_position`

const queryPrimaryKeys = `
	SELECT a.attname
	FROM pg_index i
	JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
	WHERE i.indrelid = (quote_ident($1) || '.' || quote_ident($2))::regclass
		AND i.indisprimary`

const queryForeignKeys = `
	SELECT
		tc.constraint_name,
		kcu.column_name,
		ccu.table_name AS referenced_table,
		ccu.column_name AS referenced_column
	FROM information_schema.table_constraints tc
	JOIN information_schema.key_column_usage kcu
		ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
	JOIN information_schema.constraint_column_usage ccu
		ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
	WHERE tc.constraint_type = 'FOREIGN KEY'
		AND tc.table_schema = $1
		AND tc.table_name = $2`

const queryIndexes = `
	SELECT
		indexname,
		indexdef,
		i.indisunique
	FROM pg_indexes pgi
	JOIN pg_class c ON c.relname = pgi.indexname
	JOIN pg_index i ON i.indexrelid = c.oid
	WHERE pgi.schemaname = $1 AND pgi.tablename = $2`

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

func (e *Explorer) fetchTableMeta(ctx context.Context, tableName string) (schema, comment string, err error) {
	err = e.pool.QueryRow(ctx, queryTableMeta, tableName).Scan(&schema, &comment)
	if err != nil {
		return "", "", fmt.Errorf("table %q not found: %w", tableName, err)
	}
	return schema, comment, nil
}

func (e *Explorer) fetchColumns(ctx context.Context, schema, tableName string) ([]ports.ColumnInfo, error) {
	rows, err := e.pool.Query(ctx, queryColumns, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying columns: %w", err)
	}
	defer rows.Close()

	var cols []ports.ColumnInfo
	for rows.Next() {
		var col ports.ColumnInfo
		if err := rows.Scan(&col.Name, &col.DataType, &col.IsNullable, &col.DefaultValue, &col.Comment); err != nil {
			return nil, fmt.Errorf("scanning column: %w", err)
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func (e *Explorer) markPrimaryKeys(ctx context.Context, detail *ports.TableDetail) error {
	rows, err := e.pool.Query(ctx, queryPrimaryKeys, detail.Schema, detail.Name)
	if err != nil {
		return fmt.Errorf("querying primary keys: %w", err)
	}
	defer rows.Close()

	pkCols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scanning pk: %w", err)
		}
		pkCols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range detail.Columns {
		if pkCols[detail.Columns[i].Name] {
			detail.Columns[i].IsPrimaryKey = true
		}
	}
	return nil
}

func (e *Explorer) fetchForeignKeys(ctx context.Context, schema, tableName string) ([]ports.ForeignKey, error) {
	rows, err := e.pool.Query(ctx, queryForeignKeys, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying foreign keys: %w", err)
	}
	defer rows.Close()

	var fks []ports.ForeignKey
	for rows.Next() {
		var fk ports.ForeignKey
		if err := rows.Scan(&fk.ConstraintName, &fk.ColumnName, &fk.ReferencedTable, &fk.ReferencedColumn); err != nil {
			return nil, fmt.Errorf("scanning fk: %w", err)
		}
		fks = append(fks, fk)
	}
	return fks, rows.Err()
}

func (e *Explorer) fetchIndexes(ctx context.Context, schema, tableName string) ([]ports.IndexInfo, error) {
	rows, err := e.pool.Query(ctx, queryIndexes, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying indexes: %w", err)
	}
	defer rows.Close()

	var idxs []ports.IndexInfo
	for rows.Next() {
		var idx ports.IndexInfo
		if err := rows.Scan(&idx.Name, &idx.Definition, &idx.IsUnique); err != nil {
			return nil, fmt.Errorf("scanning index: %w", err)
		}
		idxs = append(idxs, idx)
	}
	return idxs, rows.Err()
}
