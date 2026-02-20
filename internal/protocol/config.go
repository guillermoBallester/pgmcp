package protocol

import "time"

// YamuxConfig holds tunable parameters for yamux sessions.
type YamuxConfig struct {
	KeepAliveInterval      time.Duration
	ConnectionWriteTimeout time.Duration
}

// HeartbeatConfig controls the server-initiated heartbeat behavior.
type HeartbeatConfig struct {
	Interval      time.Duration // How often to send pings (default 10s).
	Timeout       time.Duration // Per-ping read/write deadline (default 5s).
	MissThreshold int           // Consecutive failures before closing session (default 3).
}

// AgentTunnelConfig holds tunable parameters for the tunnel agent.
type AgentTunnelConfig struct {
	SessionTTL             time.Duration
	SessionCleanupInterval time.Duration
	InitialBackoff         time.Duration
	MaxBackoff             time.Duration
	ForceCloseTimeout      time.Duration
	Yamux                  YamuxConfig
}

// ServerTunnelConfig holds tunable parameters for the tunnel server.
type ServerTunnelConfig struct {
	Heartbeat        HeartbeatConfig
	HandshakeTimeout time.Duration
	DiscoveryTimeout time.Duration
	Yamux            YamuxConfig
}
