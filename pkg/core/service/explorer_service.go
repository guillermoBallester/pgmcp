package service

import (
	"context"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/ports"
)

// ExplorerService wraps SchemaExplorer. Pass-through for now;
// future home for schema-level validation (e.g., table name allowlists).
type ExplorerService struct {
	explorer ports.SchemaExplorer
}

func NewExplorerService(explorer ports.SchemaExplorer) *ExplorerService {
	return &ExplorerService{
		explorer: explorer,
	}
}

func (s *ExplorerService) ListSchemas(ctx context.Context) ([]ports.SchemaInfo, error) {
	return s.explorer.ListSchemas(ctx)
}

func (s *ExplorerService) ListTables(ctx context.Context) ([]ports.TableInfo, error) {
	return s.explorer.ListTables(ctx)
}

func (s *ExplorerService) DescribeTable(ctx context.Context, schema, tableName string) (*ports.TableDetail, error) {
	return s.explorer.DescribeTable(ctx, schema, tableName)
}
