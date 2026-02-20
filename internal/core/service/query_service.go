package service

import (
	"context"
	"log/slog"

	"github.com/guillermoBallester/isthmus/internal/core/domain"
	"github.com/guillermoBallester/isthmus/internal/core/port"
)

// QueryService orchestrates SQL validation (domain) and execution (infrastructure).
type QueryService struct {
	validator *domain.QueryValidator
	executor  port.QueryExecutor
	logger    *slog.Logger
}

func NewQueryService(validator *domain.QueryValidator, executor port.QueryExecutor, logger *slog.Logger) *QueryService {
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

	return s.executor.Execute(ctx, sql)
}
