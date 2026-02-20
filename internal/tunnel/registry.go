package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	"github.com/hashicorp/yamux"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ErrNoTunnel is returned when no agent tunnel is connected for a given database.
var ErrNoTunnel = errors.New("no tunnel connected for database")

// tunnelEntry holds a single agent tunnel and its associated per-database resources.
type tunnelEntry struct {
	databaseID   uuid.UUID
	yamuxSession *yamux.Session
	mcpServer    *mcpserver.MCPServer
	proxy        *Proxy
	heartbeatCtx context.CancelFunc
}

// TunnelRegistry manages multiple simultaneous agent tunnels keyed by database ID.
type TunnelRegistry struct {
	logger        *slog.Logger
	authenticator auth.Authenticator
	cfg           tunnel.ServerTunnelConfig
	serverVersion string

	mu      sync.RWMutex
	tunnels map[uuid.UUID]*tunnelEntry
}

// NewTunnelRegistry creates a new multi-tunnel registry.
func NewTunnelRegistry(authenticator auth.Authenticator, cfg tunnel.ServerTunnelConfig, serverVersion string, logger *slog.Logger) *TunnelRegistry {
	return &TunnelRegistry{
		logger:        logger,
		authenticator: authenticator,
		cfg:           cfg,
		serverVersion: serverVersion,
		tunnels:       make(map[uuid.UUID]*tunnelEntry),
	}
}

// GetMCPServer returns the per-database MCPServer for the given database ID.
// Returns nil if no tunnel is connected for that database.
func (r *TunnelRegistry) GetMCPServer(databaseID uuid.UUID) *mcpserver.MCPServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.tunnels[databaseID]; ok {
		return entry.mcpServer
	}
	return nil
}

// Connected returns true if a tunnel is connected for the given database.
func (r *TunnelRegistry) Connected(databaseID uuid.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tunnels[databaseID]
	return ok
}

// AnyConnected returns true if at least one tunnel is active.
func (r *TunnelRegistry) AnyConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tunnels) > 0
}

// HandleTunnel is the HTTP handler for the /tunnel WebSocket endpoint.
// It authenticates the agent, determines which database it represents,
// and manages the yamux session lifecycle.
func (r *TunnelRegistry) HandleTunnel(w http.ResponseWriter, req *http.Request) {
	authResult := r.authenticate(req)
	if authResult == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Agent API key must be linked to exactly one database.
	if len(authResult.DatabaseIDs) == 0 {
		http.Error(w, "api key has no database assigned", http.StatusForbidden)
		return
	}
	if len(authResult.DatabaseIDs) > 1 {
		http.Error(w, "agent api key must be linked to exactly one database", http.StatusForbidden)
		return
	}

	databaseID := authResult.DatabaseIDs[0]

	// Check for duplicate tunnel.
	r.mu.RLock()
	_, exists := r.tunnels[databaseID]
	r.mu.RUnlock()
	if exists {
		http.Error(w, "tunnel already connected for this database", http.StatusConflict)
		return
	}

	wsConn, err := websocket.Accept(w, req, nil)
	if err != nil {
		r.logger.Error("websocket accept failed", slog.String("error", err.Error()))
		return
	}
	defer wsConn.CloseNow() //nolint:errcheck

	netConn := websocket.NetConn(req.Context(), wsConn, websocket.MessageBinary)

	// Cloud server is yamux CLIENT — it opens streams to the agent.
	session, err := yamux.Client(netConn, tunnel.NewYamuxConfig(r.cfg.Yamux))
	if err != nil {
		r.logger.Error("yamux client creation failed", slog.String("error", err.Error()))
		return
	}
	defer session.Close() //nolint:errcheck

	// Perform handshake.
	ack, err := r.performHandshake(session)
	if err != nil {
		r.logger.Error("handshake failed", slog.String("error", err.Error()))
		return
	}
	if ack.Error != "" {
		r.logger.Error("agent rejected handshake", slog.String("error", ack.Error))
		return
	}

	r.logger.Info("handshake completed",
		slog.String("database_id", databaseID.String()),
		slog.Uint64("protocol_version", uint64(ack.ProtocolVersion)),
		slog.String("agent_version", ack.AgentVersion),
	)

	// Create a per-database MCPServer + Proxy.
	mcpSrv := mcpserver.NewMCPServer("isthmus-cloud", r.serverVersion,
		mcpserver.WithToolCapabilities(true),
	)

	proxy := NewProxy(session, mcpSrv, r.cfg, r.logger.With(
		slog.String("database_id", databaseID.String()),
	))

	// Discover tools from the agent.
	if err := proxy.DiscoverAndRegister(); err != nil {
		r.logger.Error("tool discovery failed",
			slog.String("database_id", databaseID.String()),
			slog.String("error", err.Error()),
		)
		return
	}

	// Start heartbeat.
	heartbeatCtx, heartbeatCancel := context.WithCancel(req.Context())
	defer heartbeatCancel()
	go r.runHeartbeat(heartbeatCtx, session, databaseID)

	// Register in the map.
	entry := &tunnelEntry{
		databaseID:   databaseID,
		yamuxSession: session,
		mcpServer:    mcpSrv,
		proxy:        proxy,
		heartbeatCtx: heartbeatCancel,
	}

	r.mu.Lock()
	r.tunnels[databaseID] = entry
	r.mu.Unlock()

	r.logger.Info("agent connected",
		slog.String("database_id", databaseID.String()),
	)

	// Block until the agent disconnects.
	<-session.CloseChan()

	// Cleanup.
	proxy.Cleanup()

	r.mu.Lock()
	delete(r.tunnels, databaseID)
	r.mu.Unlock()

	r.logger.Info("agent disconnected",
		slog.String("database_id", databaseID.String()),
	)
}

// runHeartbeat sends pings at the configured interval and closes the session
// if too many consecutive pings fail.
func (r *TunnelRegistry) runHeartbeat(ctx context.Context, session *yamux.Session, databaseID uuid.UUID) {
	ticker := time.NewTicker(r.cfg.Heartbeat.Interval)
	defer ticker.Stop()

	misses := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rtt, draining, err := r.sendPing(session)
			if err != nil {
				misses++
				r.logger.Warn("heartbeat ping failed",
					slog.String("database_id", databaseID.String()),
					slog.Int("misses", misses),
					slog.Int("threshold", r.cfg.Heartbeat.MissThreshold),
					slog.String("error", err.Error()),
				)
				if misses >= r.cfg.Heartbeat.MissThreshold {
					r.logger.Error("heartbeat miss threshold exceeded, closing session",
						slog.String("database_id", databaseID.String()),
					)
					session.Close() //nolint:errcheck
					return
				}
				continue
			}

			misses = 0
			r.logger.Debug("heartbeat pong received",
				slog.String("database_id", databaseID.String()),
				slog.Duration("rtt", rtt),
				slog.Bool("draining", draining),
			)
		}
	}
}

// sendPing opens a stream, writes a Ping, reads a Pong, and returns RTT and draining status.
func (r *TunnelRegistry) sendPing(session *yamux.Session) (rtt time.Duration, draining bool, err error) {
	stream, err := session.Open()
	if err != nil {
		return 0, false, fmt.Errorf("open heartbeat stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck

	deadline := time.Now().Add(r.cfg.Heartbeat.Timeout)
	if err := stream.SetDeadline(deadline); err != nil {
		return 0, false, fmt.Errorf("set deadline: %w", err)
	}

	now := time.Now()
	ping := &tunnel.Ping{Timestamp: now.UnixNano()}
	if err := tunnel.WritePing(stream, ping); err != nil {
		return 0, false, fmt.Errorf("write ping: %w", err)
	}

	pong, err := tunnel.ReadPong(stream)
	if err != nil {
		return 0, false, fmt.Errorf("read pong: %w", err)
	}

	rtt = time.Since(now)
	return rtt, pong.Draining, nil
}

// performHandshake opens a yamux stream, writes a Handshake, and reads a HandshakeAck.
func (r *TunnelRegistry) performHandshake(session *yamux.Session) (*tunnel.HandshakeAck, error) {
	stream, err := session.Open()
	if err != nil {
		return nil, fmt.Errorf("open handshake stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck

	deadline := time.Now().Add(r.cfg.HandshakeTimeout)
	if err := stream.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set handshake deadline: %w", err)
	}

	h := &tunnel.Handshake{
		ProtocolVersion: tunnel.ProtocolVersion,
		ServerVersion:   r.serverVersion,
	}
	if err := tunnel.WriteHandshake(stream, h); err != nil {
		return nil, fmt.Errorf("write handshake: %w", err)
	}

	ack, err := tunnel.ReadHandshakeAck(stream)
	if err != nil {
		return nil, fmt.Errorf("read handshake ack: %w", err)
	}

	return ack, nil
}

func (r *TunnelRegistry) authenticate(req *http.Request) *auth.AuthResult {
	header := req.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil
	}
	token := strings.TrimPrefix(header, "Bearer ")
	result, err := r.authenticator.Authenticate(req.Context(), token)
	if err != nil {
		r.logger.Error("authentication error", slog.String("error", err.Error()))
		return nil
	}
	return result
}
