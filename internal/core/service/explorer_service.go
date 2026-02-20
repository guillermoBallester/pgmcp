package service

import (
	"context"

	"github.com/guillermoBallester/isthmus/internal/core/port"
)

// ExplorerService wraps SchemaExplorer. Pass-through for now;
// future home for schema-level validation (e.g., table name allowlists).
type ExplorerService struct {
	explorer port.SchemaExplorer
}

func NewExplorerService(explorer port.SchemaExplorer) *ExplorerService {
	return &ExplorerService{
		explorer: explorer,
	}
}

func (s *ExplorerService) ListSchemas(ctx context.Context) ([]port.SchemaInfo, error) {
	return s.explorer.ListSchemas(ctx)
}

func (s *ExplorerService) ListTables(ctx context.Context) ([]port.TableInfo, error) {
	return s.explorer.ListTables(ctx)
}

func (s *ExplorerService) DescribeTable(ctx context.Context, schema, tableName string) (*port.TableDetail, error) {
	return s.explorer.DescribeTable(ctx, schema, tableName)
}
