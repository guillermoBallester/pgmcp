package config

import (
	"fmt"
	"log/slog"
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
	LogLevel     slog.Level
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

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level, err := parseLogLevel(v)
		if err != nil {
			return nil, err
		}
		cfg.LogLevel = level
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

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid LOG_LEVEL value %q: must be debug, info, warn, or error", s)
	}
}
