package port

import "context"

type TableInfo struct {
	Schema      string `json:"schema"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	RowEstimate int64  `json:"row_estimate"`
	Comment     string `json:"comment,omitempty"`
}

type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsNullable   bool   `json:"is_nullable"`
	DefaultValue string `json:"default_value,omitempty"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	Comment      string `json:"comment,omitempty"`
}

type ForeignKey struct {
	ConstraintName   string `json:"constraint_name"`
	ColumnName       string `json:"column_name"`
	ReferencedTable  string `json:"referenced_table"`
	ReferencedColumn string `json:"referenced_column"`
}

type IndexInfo struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	IsUnique   bool   `json:"is_unique"`
}

type TableDetail struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Comment     string       `json:"comment,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
	Indexes     []IndexInfo  `json:"indexes,omitempty"`
}

type SchemaInfo struct {
	Name string `json:"name"`
}

type SchemaExplorer interface {
	ListSchemas(ctx context.Context) ([]SchemaInfo, error)
	ListTables(ctx context.Context) ([]TableInfo, error)
	DescribeTable(ctx context.Context, schema, tableName string) (*TableDetail, error)
}
