package service

import (
	"context"

	"github.com/guillermoballestersasso/pgmcp/internal/core/domain"
	"github.com/guillermoballestersasso/pgmcp/internal/core/ports"
)

// QueryService orchestrates SQL validation (domain) and execution (infrastructure).
type QueryService struct {
	validator *domain.QueryValidator
	executor  ports.QueryExecutor
}

func NewQueryService(validator *domain.QueryValidator, executor ports.QueryExecutor) *QueryService {
	return &QueryService{
		validator: validator,
		executor:  executor,
	}
}

// Execute validates the SQL statement and, if allowed, delegates to the executor.
func (s *QueryService) Execute(ctx context.Context, sql string) ([]map[string]any, error) {
	if err := s.validator.Validate(sql); err != nil {
		return nil, err
	}
	return s.executor.Execute(ctx, sql)
}
