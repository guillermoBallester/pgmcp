package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/ports"
)

// ExplorerService wraps SchemaExplorer. Pass-through for now;
// future home for schema-level validation (e.g., table name allowlists).
type ExplorerService struct {
	explorer ports.SchemaExplorer
	logger   *slog.Logger
}

func NewExplorerService(explorer ports.SchemaExplorer, logger *slog.Logger) *ExplorerService {
	return &ExplorerService{
		explorer: explorer,
		logger:   logger,
	}
}

func (s *ExplorerService) ListSchemas(ctx context.Context) ([]ports.SchemaInfo, error) {
	start := time.Now()
	schemas, err := s.explorer.ListSchemas(ctx)
	duration := time.Since(start)

	if err != nil {
		s.logger.ErrorContext(ctx, "list schemas failed",
			slog.String("db.operation.name", "list_schemas"),
			slog.Duration("duration", duration),
			slog.String("error.type", "query_error"),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "list schemas",
		slog.String("db.operation.name", "list_schemas"),
		slog.Int("db.response.rows", len(schemas)),
		slog.Duration("duration", duration),
	)
	return schemas, nil
}

func (s *ExplorerService) ListTables(ctx context.Context) ([]ports.TableInfo, error) {
	start := time.Now()
	tables, err := s.explorer.ListTables(ctx)
	duration := time.Since(start)

	if err != nil {
		s.logger.ErrorContext(ctx, "list tables failed",
			slog.String("db.operation.name", "list_tables"),
			slog.Duration("duration", duration),
			slog.String("error.type", "query_error"),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "list tables",
		slog.String("db.operation.name", "list_tables"),
		slog.Int("db.response.rows", len(tables)),
		slog.Duration("duration", duration),
	)
	return tables, nil
}

func (s *ExplorerService) DescribeTable(ctx context.Context, schema, tableName string) (*ports.TableDetail, error) {
	start := time.Now()
	detail, err := s.explorer.DescribeTable(ctx, schema, tableName)
	duration := time.Since(start)

	if err != nil {
		s.logger.ErrorContext(ctx, "describe table failed",
			slog.String("db.operation.name", "describe_table"),
			slog.String("db.collection.name", tableName),
			slog.Duration("duration", duration),
			slog.String("error.type", "query_error"),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "describe table",
		slog.String("db.operation.name", "describe_table"),
		slog.String("db.collection.name", tableName),
		slog.Int("db.response.columns", len(detail.Columns)),
		slog.Duration("duration", duration),
	)
	return detail, nil
}
