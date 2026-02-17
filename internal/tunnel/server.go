package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
	"github.com/hashicorp/yamux"
)

// ErrNoAgent is returned when a call is forwarded but no agent is connected.
var ErrNoAgent = errors.New("no agent connected")

// HeartbeatConfig controls the server-initiated heartbeat behavior.
type HeartbeatConfig struct {
	Interval      time.Duration // How often to send pings (default 10s).
	Timeout       time.Duration // Per-ping read/write deadline (default 5s).
	MissThreshold int           // Consecutive failures before closing session (default 3).
}

// DefaultHeartbeatConfig returns sensible defaults for heartbeat.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	}
}

// HandshakeTimeout is the maximum time allowed for the handshake exchange.
const HandshakeTimeout = 10 * time.Second

// TunnelServer manages the WebSocket connection to the agent and forwards
// MCP calls through the yamux tunnel.
type TunnelServer struct {
	logger        *slog.Logger
	apiKeys       map[string]bool
	hbCfg         HeartbeatConfig
	serverVersion string

	mu           sync.RWMutex
	yamuxSession *yamux.Session

	onConnect    func()
	onDisconnect func()
}

// NewTunnelServer creates a new tunnel server with the given API keys, heartbeat config, and server version.
func NewTunnelServer(apiKeys []string, hbCfg HeartbeatConfig, serverVersion string, logger *slog.Logger) *TunnelServer {
	keySet := make(map[string]bool, len(apiKeys))
	for _, k := range apiKeys {
		keySet[k] = true
	}
	return &TunnelServer{
		logger:        logger,
		apiKeys:       keySet,
		hbCfg:         hbCfg,
		serverVersion: serverVersion,
	}
}

// SetOnConnect sets a callback invoked when an agent connects.
func (s *TunnelServer) SetOnConnect(fn func()) {
	s.onConnect = fn
}

// SetOnDisconnect sets a callback invoked when an agent disconnects.
func (s *TunnelServer) SetOnDisconnect(fn func()) {
	s.onDisconnect = fn
}

// Connected returns true if an agent is currently connected.
func (s *TunnelServer) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.yamuxSession != nil
}

// HandleTunnel is the HTTP handler for the /tunnel WebSocket endpoint.
func (s *TunnelServer) HandleTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	wsConn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Error("websocket accept failed",
			slog.String("error", err.Error()),
		)
		return
	}
	defer wsConn.CloseNow() //nolint:errcheck // best-effort cleanup

	netConn := websocket.NetConn(r.Context(), wsConn, websocket.MessageBinary)

	// Cloud server is yamux CLIENT — it opens streams to the agent.
	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.LogOutput = io.Discard
	session, err := yamux.Client(netConn, yamuxCfg)
	if err != nil {
		s.logger.Error("yamux client creation failed",
			slog.String("error", err.Error()),
		)
		return
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup

	// Perform handshake before accepting any other traffic.
	ack, err := s.performHandshake(session)
	if err != nil {
		s.logger.Error("handshake failed",
			slog.String("error", err.Error()),
		)
		return
	}
	if ack.Error != "" {
		s.logger.Error("agent rejected handshake",
			slog.String("error", ack.Error),
		)
		return
	}

	s.logger.Info("handshake completed",
		slog.Uint64("protocol_version", uint64(ack.ProtocolVersion)),
		slog.String("agent_version", ack.AgentVersion),
	)

	s.mu.Lock()
	s.yamuxSession = session
	s.mu.Unlock()

	s.logger.Info("agent connected")

	if s.onConnect != nil {
		s.onConnect()
	}

	// Start heartbeat goroutine.
	heartbeatCtx, heartbeatCancel := context.WithCancel(r.Context())
	defer heartbeatCancel()
	go s.runHeartbeat(heartbeatCtx, session)

	// Block until the agent disconnects.
	<-session.CloseChan()

	s.mu.Lock()
	s.yamuxSession = nil
	s.mu.Unlock()

	s.logger.Info("agent disconnected")

	if s.onDisconnect != nil {
		s.onDisconnect()
	}
}

// ForwardCall sends a JSON-RPC payload to the agent through a new yamux stream
// and returns the response.
func (s *TunnelServer) ForwardCall(ctx context.Context, sessionID string, payload json.RawMessage) (json.RawMessage, error) {
	s.mu.RLock()
	session := s.yamuxSession
	s.mu.RUnlock()

	if session == nil {
		return nil, ErrNoAgent
	}

	stream, err := session.Open()
	if err != nil {
		return nil, fmt.Errorf("open yamux stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck // best-effort cleanup

	req := tunnel.Request{
		SessionID: sessionID,
		Payload:   payload,
	}
	if err := tunnel.WriteRequest(stream, &req); err != nil {
		return nil, fmt.Errorf("write tunnel request: %w", err)
	}

	resp, err := tunnel.ReadResponse(stream)
	if err != nil {
		return nil, fmt.Errorf("read tunnel response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}

	return resp.Payload, nil
}

// runHeartbeat sends pings at the configured interval and closes the session
// if too many consecutive pings fail.
func (s *TunnelServer) runHeartbeat(ctx context.Context, session *yamux.Session) {
	ticker := time.NewTicker(s.hbCfg.Interval)
	defer ticker.Stop()

	misses := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rtt, draining, err := s.sendPing(session)
			if err != nil {
				misses++
				s.logger.Warn("heartbeat ping failed",
					slog.Int("misses", misses),
					slog.Int("threshold", s.hbCfg.MissThreshold),
					slog.String("error", err.Error()),
				)
				if misses >= s.hbCfg.MissThreshold {
					s.logger.Error("heartbeat miss threshold exceeded, closing session")
					session.Close() //nolint:errcheck
					return
				}
				continue
			}

			misses = 0
			s.logger.Debug("heartbeat pong received",
				slog.Duration("rtt", rtt),
				slog.Bool("draining", draining),
			)
		}
	}
}

// sendPing opens a stream, writes a Ping, reads a Pong, and returns RTT and draining status.
func (s *TunnelServer) sendPing(session *yamux.Session) (rtt time.Duration, draining bool, err error) {
	stream, err := session.Open()
	if err != nil {
		return 0, false, fmt.Errorf("open heartbeat stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck

	deadline := time.Now().Add(s.hbCfg.Timeout)
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
func (s *TunnelServer) performHandshake(session *yamux.Session) (*tunnel.HandshakeAck, error) {
	stream, err := session.Open()
	if err != nil {
		return nil, fmt.Errorf("open handshake stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck

	deadline := time.Now().Add(HandshakeTimeout)
	if err := stream.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set handshake deadline: %w", err)
	}

	h := &tunnel.Handshake{
		ProtocolVersion: tunnel.ProtocolVersion,
		ServerVersion:   s.serverVersion,
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

func (s *TunnelServer) authenticate(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	return s.apiKeys[token]
}
