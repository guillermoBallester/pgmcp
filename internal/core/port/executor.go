package port

import "context"

type QueryExecutor interface {
	Execute(ctx context.Context, sql string) ([]map[string]any, error)
}
