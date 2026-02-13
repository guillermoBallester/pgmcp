package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL  string
	ReadOnly     bool
	MaxRows      int
	QueryTimeout time.Duration
	Schemas      []string // empty means all non-system schemas
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		ReadOnly:     true,
		MaxRows:      100,
		QueryTimeout: 10 * time.Second,
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	if v := os.Getenv("READ_ONLY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid READ_ONLY value %q: %w", v, err)
		}
		cfg.ReadOnly = b
	}

	if v := os.Getenv("MAX_ROWS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid MAX_ROWS value %q: must be a positive integer", v)
		}
		cfg.MaxRows = n
	}

	if v := os.Getenv("QUERY_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid QUERY_TIMEOUT value %q: %w", v, err)
		}
		cfg.QueryTimeout = d
	}

	if v := os.Getenv("SCHEMAS"); v != "" {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.Schemas = append(cfg.Schemas, s)
			}
		}
	}

	return cfg, nil
}
