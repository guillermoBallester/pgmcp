package postgres

import (
	"context"
	"fmt"

	"github.com/guillermoBallester/isthmus/internal/core/port"
)

func (e *Explorer) fetchTableComment(ctx context.Context, schema, tableName string) (string, error) {
	var comment string
	err := e.pool.QueryRow(ctx, queryTableComment, schema, tableName).Scan(&comment)
	if err != nil {
		return "", fmt.Errorf("table %q not found in schema %q: %w", tableName, schema, err)
	}
	return comment, nil
}

func (e *Explorer) fetchTableMeta(ctx context.Context, tableName string) (schema, comment string, err error) {
	filter, filterArgs := e.schemaFilter("t.table_schema", 2) // $1 is tableName
	query := fmt.Sprintf(queryTableMeta, filter)

	args := make([]any, 0, 1+len(filterArgs))
	args = append(args, tableName)
	args = append(args, filterArgs...)

	err = e.pool.QueryRow(ctx, query, args...).Scan(&schema, &comment)
	if err != nil {
		if len(e.schemas) > 0 {
			return "", "", fmt.Errorf("table %q not found in schemas %v: %w", tableName, e.schemas, err)
		}
		return "", "", fmt.Errorf("table %q not found: %w", tableName, err)
	}
	return schema, comment, nil
}

func (e *Explorer) fetchColumns(ctx context.Context, schema, tableName string) ([]port.ColumnInfo, error) {
	rows, err := e.pool.Query(ctx, queryColumns, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying columns: %w", err)
	}
	defer rows.Close()

	var cols []port.ColumnInfo
	for rows.Next() {
		var col port.ColumnInfo
		if err := rows.Scan(&col.Name, &col.DataType, &col.IsNullable, &col.DefaultValue, &col.Comment); err != nil {
			return nil, fmt.Errorf("scanning column: %w", err)
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func (e *Explorer) markPrimaryKeys(ctx context.Context, detail *port.TableDetail) error {
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

func (e *Explorer) fetchForeignKeys(ctx context.Context, schema, tableName string) ([]port.ForeignKey, error) {
	rows, err := e.pool.Query(ctx, queryForeignKeys, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying foreign keys: %w", err)
	}
	defer rows.Close()

	var fks []port.ForeignKey
	for rows.Next() {
		var fk port.ForeignKey
		if err := rows.Scan(&fk.ConstraintName, &fk.ColumnName, &fk.ReferencedTable, &fk.ReferencedColumn); err != nil {
			return nil, fmt.Errorf("scanning fk: %w", err)
		}
		fks = append(fks, fk)
	}
	return fks, rows.Err()
}

func (e *Explorer) fetchIndexes(ctx context.Context, schema, tableName string) ([]port.IndexInfo, error) {
	rows, err := e.pool.Query(ctx, queryIndexes, schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying indexes: %w", err)
	}
	defer rows.Close()

	var idxs []port.IndexInfo
	for rows.Next() {
		var idx port.IndexInfo
		if err := rows.Scan(&idx.Name, &idx.Definition, &idx.IsUnique); err != nil {
			return nil, fmt.Errorf("scanning index: %w", err)
		}
		idxs = append(idxs, idx)
	}
	return idxs, rows.Err()
}
