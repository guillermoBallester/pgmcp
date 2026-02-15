package domain

import (
	"errors"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

var (
	ErrEmptyQuery     = errors.New("empty query")
	ErrNotAllowed     = errors.New("only SELECT queries are allowed")
	ErrMultiStatement = errors.New("multiple statements are not allowed")
)

// QueryValidator validates SQL statements using PostgreSQL's actual parser.
// Only SELECT statements are permitted (whitelist approach).
type QueryValidator struct{}

func NewQueryValidator() *QueryValidator {
	return &QueryValidator{}
}

// Validate parses the SQL and rejects anything that isn't a single SELECT statement.
func (v *QueryValidator) Validate(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return ErrEmptyQuery
	}

	tree, err := pg_query.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("failed to parse SQL: %w", err)
	}

	if len(tree.Stmts) == 0 {
		return ErrEmptyQuery
	}

	if len(tree.Stmts) > 1 {
		return ErrMultiStatement
	}

	stmt := tree.Stmts[0].Stmt
	if stmt == nil {
		return ErrEmptyQuery
	}

	switch stmt.Node.(type) {
	case *pg_query.Node_SelectStmt:
		return nil
	case *pg_query.Node_ExplainStmt:
		return nil
	default:
		return ErrNotAllowed
	}
}
