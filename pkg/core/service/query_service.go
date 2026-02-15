package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/domain"
	"github.com/guillermoballestersasso/pgmcp/pkg/core/ports"
)

// QueryService orchestrates SQL validation (domain) and execution (infrastructure).
type QueryService struct {
	validator *domain.QueryValidator
	executor  ports.QueryExecutor
	logger    *slog.Logger
}

func NewQueryService(validator *domain.QueryValidator, executor ports.QueryExecutor, logger *slog.Logger) *QueryService {
	return &QueryService{
		validator: validator,
		executor:  executor,
		logger:    logger,
	}
}

// Execute validates the SQL statement and, if allowed, delegates to the executor.
func (s *QueryService) Execute(ctx context.Context, sql string) ([]map[string]any, error) {
	if err := s.validator.Validate(sql); err != nil {
		s.logger.WarnContext(ctx, "query validation rejected",
			slog.String("db.operation.name", "query"),
			slog.String("db.statement", sql),
			slog.String("error.type", "validation_error"),
		)
		return nil, err
	}

	s.logger.DebugContext(ctx, "executing query",
		slog.String("db.operation.name", "query"),
		slog.String("db.system", "postgresql"),
		slog.String("db.statement", sql),
	)

	start := time.Now()
	rows, err := s.executor.Execute(ctx, sql)
	duration := time.Since(start)

	if err != nil {
		s.logger.ErrorContext(ctx, "query execution failed",
			slog.String("db.operation.name", "query"),
			slog.String("db.statement", sql),
			slog.Duration("duration", duration),
			slog.String("error.type", "query_error"),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "query executed",
		slog.String("db.operation.name", "query"),
		slog.String("db.system", "postgresql"),
		slog.Int("db.response.rows", len(rows)),
		slog.Duration("duration", duration),
	)
	return rows, nil
}
