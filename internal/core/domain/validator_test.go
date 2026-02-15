package domain

import (
	"errors"
	"testing"
)

// errAny is a sentinel meaning "any error is acceptable".
var errAny = errors.New("any error")

func TestQueryValidator_Validate(t *testing.T) {
	v := NewQueryValidator()

	tests := []struct {
		name    string
		sql     string
		wantErr error
	}{
		// Valid SELECT statements
		{"simple select", "SELECT 1", nil},
		{"select from table", "SELECT id, name FROM users", nil},
		{"select with join", "SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id", nil},
		{"select with where", "SELECT * FROM users WHERE id = 1", nil},
		{"select with union", "SELECT 1 UNION SELECT 2", nil},
		{"select with CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte", nil},
		{"select with subquery", "SELECT * FROM (SELECT 1) AS t", nil},
		{"select with group by", "SELECT count(*) FROM users GROUP BY status", nil},
		{"explain select", "EXPLAIN SELECT 1", nil},
		{"explain analyze select", "EXPLAIN ANALYZE SELECT * FROM users", nil},

		// Rejected: DDL
		{"drop table", "DROP TABLE users", ErrNotAllowed},
		{"create table", "CREATE TABLE t (id int)", ErrNotAllowed},
		{"alter table", "ALTER TABLE users ADD COLUMN age int", ErrNotAllowed},
		{"truncate", "TRUNCATE users", ErrNotAllowed},
		{"create index", "CREATE INDEX idx ON users(id)", ErrNotAllowed},

		// Rejected: DML
		{"insert", "INSERT INTO users (name) VALUES ('a')", ErrNotAllowed},
		{"update", "UPDATE users SET name = 'a'", ErrNotAllowed},
		{"delete", "DELETE FROM users", ErrNotAllowed},

		// Rejected: privileges
		{"grant", "GRANT SELECT ON users TO public", ErrNotAllowed},
		{"revoke", "REVOKE SELECT ON users FROM public", ErrNotAllowed},

		// Rejected: admin
		{"copy", "COPY users TO '/tmp/out.csv'", ErrNotAllowed},
		{"vacuum", "VACUUM users", ErrNotAllowed},

		// Rejected: procedural
		{"do block", "DO $$ BEGIN RAISE NOTICE 'hi'; END $$", ErrNotAllowed},

		// Rejected: transaction control
		{"begin", "BEGIN", ErrNotAllowed},
		{"commit", "COMMIT", ErrNotAllowed},
		{"rollback", "ROLLBACK", ErrNotAllowed},

		// Edge cases
		{"empty string", "", ErrEmptyQuery},
		{"whitespace only", "   ", ErrEmptyQuery},
		{"multiple statements", "SELECT 1; SELECT 2", ErrMultiStatement},
		{"select then drop", "SELECT 1; DROP TABLE users", ErrMultiStatement},

		// Comment-obfuscated DDL (parser rejects as invalid SQL — defense in depth)
		{"comment obfuscated drop", "DR/**/OP TABLE users", errAny}, // parse error = rejected
		{"inline comment select", "SELECT /* comment */ 1", nil},
		{"line comment select", "-- comment\nSELECT 1", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.sql)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if tt.wantErr == errAny {
				return // any error is acceptable
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got: %v", tt.wantErr, err)
			}
		})
	}
}
