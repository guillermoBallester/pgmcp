package config

import "time"

// HTTPConfig holds tunable parameters for the HTTP server.
type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}
