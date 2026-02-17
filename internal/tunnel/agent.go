package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/server"
)

// Agent connects to the cloud server via WebSocket and serves MCP calls
// received through yamux streams.
type Agent struct {
	tunnelURL    string
	apiKey       string
	agentVersion string
	mcpServer    *server.MCPServer
	logger       *slog.Logger

	mu       sync.Mutex
	sessions map[string]*server.InProcessSession

	drainMu  sync.Mutex     // protects draining check + wg.Add atomicity
	wg       sync.WaitGroup // tracks in-flight handleStream goroutines
	draining atomic.Bool    // true when shutdown initiated
}

// NewAgent creates a new tunnel agent.
func NewAgent(tunnelURL, apiKey, agentVersion string, mcpServer *server.MCPServer, logger *slog.Logger) *Agent {
	return &Agent{
		tunnelURL:    tunnelURL,
		apiKey:       apiKey,
		agentVersion: agentVersion,
		mcpServer:    mcpServer,
		logger:       logger,
		sessions:     make(map[string]*server.InProcessSession),
	}
}

// Run connects to the cloud server and serves MCP calls. It reconnects
// with exponential backoff on failure. Returns when ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		err := a.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		a.logger.Warn("tunnel disconnected, reconnecting",
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

// Shutdown initiates a graceful shutdown. It sets the draining flag and waits
// for all in-flight handlers to complete. Returns nil if all handlers finish
// before ctx deadline, or ctx.Err() otherwise.
func (a *Agent) Shutdown(ctx context.Context) error {
	// Lock drainMu to ensure no new wg.Add calls can happen in the accept
	// loop after we set draining. This prevents a race between wg.Add and
	// wg.Wait when the counter is zero.
	a.drainMu.Lock()
	a.draining.Store(true)
	a.drainMu.Unlock()

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) connectAndServe(ctx context.Context) error {
	a.logger.Info("connecting to tunnel server",
		slog.String("url", a.tunnelURL),
	)

	// Dial with parent context (respects cancellation during connection).
	wsConn, _, err := websocket.Dial(ctx, a.tunnelURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + a.apiKey},
		},
	})
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	defer wsConn.CloseNow() //nolint:errcheck // best-effort cleanup

	// Use a separate context for the connection lifetime so that cancelling
	// the parent ctx doesn't immediately tear down the WebSocket. This allows
	// in-flight handlers to finish during graceful shutdown.
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()

	netConn := websocket.NetConn(connCtx, wsConn, websocket.MessageBinary)

	// Agent is yamux SERVER — the cloud server opens streams to us.
	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.LogOutput = io.Discard
	session, err := yamux.Server(netConn, yamuxCfg)
	if err != nil {
		return fmt.Errorf("yamux server: %w", err)
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup

	a.logger.Info("tunnel connected")

	// When the parent context is cancelled, set draining, wait for in-flight
	// handlers to complete, then close the session to break the accept loop.
	go func() {
		<-ctx.Done()
		a.drainMu.Lock()
		a.draining.Store(true)
		a.drainMu.Unlock()
		a.wg.Wait()
		connCancel()
		session.Close() //nolint:errcheck
	}()

	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("yamux accept: %w", err)
		}

		// Lock drainMu to ensure atomicity of the draining check + wg.Add,
		// preventing a race with the drain goroutine's wg.Wait.
		a.drainMu.Lock()
		if a.draining.Load() {
			a.drainMu.Unlock()
			// Still handle pings during drain (they report draining=true
			// to the server), but don't track in WaitGroup since the drain
			// goroutine may already be in wg.Wait.
			go a.handleStream(connCtx, stream)
			continue
		}
		a.wg.Add(1)
		a.drainMu.Unlock()

		go func() {
			defer a.wg.Done()
			a.handleStream(connCtx, stream)
		}()
	}
}

func (a *Agent) handleStream(ctx context.Context, stream net.Conn) {
	defer stream.Close() //nolint:errcheck // best-effort cleanup

	msgType, payload, err := tunnel.ReadRawFrame(stream)
	if err != nil {
		a.logger.Error("failed to read tunnel frame",
			slog.String("error", err.Error()),
		)
		return
	}

	switch msgType {
	case tunnel.MessageTypeHandshake:
		a.handleHandshake(stream, payload)
	case tunnel.MessageTypePing:
		a.handlePing(stream, payload)
	case tunnel.MessageTypeRequest:
		a.handleRequest(ctx, stream, payload)
	default:
		a.logger.Warn("unknown message type",
			slog.Int("type", int(msgType)),
		)
	}
}

func (a *Agent) handleHandshake(stream net.Conn, payload json.RawMessage) {
	var h tunnel.Handshake
	if err := json.Unmarshal(payload, &h); err != nil {
		a.logger.Error("failed to unmarshal handshake",
			slog.String("error", err.Error()),
		)
		return
	}

	ack := &tunnel.HandshakeAck{
		ProtocolVersion: tunnel.ProtocolVersion,
		AgentVersion:    a.agentVersion,
	}

	// Check protocol version compatibility (exact match for now).
	if h.ProtocolVersion != tunnel.ProtocolVersion {
		ack.Error = fmt.Sprintf("incompatible protocol version: server=%d, agent=%d", h.ProtocolVersion, tunnel.ProtocolVersion)
		a.logger.Error("handshake version mismatch",
			slog.Uint64("server_version", uint64(h.ProtocolVersion)),
			slog.Uint64("agent_version", uint64(tunnel.ProtocolVersion)),
		)
	} else {
		a.logger.Info("handshake received",
			slog.Uint64("protocol_version", uint64(h.ProtocolVersion)),
			slog.String("server_version", h.ServerVersion),
		)
	}

	if err := tunnel.WriteHandshakeAck(stream, ack); err != nil {
		a.logger.Error("failed to write handshake ack",
			slog.String("error", err.Error()),
		)
	}
}

func (a *Agent) handlePing(stream net.Conn, payload json.RawMessage) {
	var ping tunnel.Ping
	if err := json.Unmarshal(payload, &ping); err != nil {
		a.logger.Error("failed to unmarshal ping",
			slog.String("error", err.Error()),
		)
		return
	}

	pong := &tunnel.Pong{
		Timestamp: ping.Timestamp,
		Draining:  a.draining.Load(),
	}

	if err := tunnel.WritePong(stream, pong); err != nil {
		a.logger.Error("failed to write pong",
			slog.String("error", err.Error()),
		)
	}
}

func (a *Agent) handleRequest(ctx context.Context, stream net.Conn, payload json.RawMessage) {
	var req tunnel.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		a.logger.Error("failed to unmarshal tunnel request",
			slog.String("error", err.Error()),
		)
		return
	}

	if err := req.Validate(); err != nil {
		a.logger.Error("invalid tunnel request",
			slog.String("error", err.Error()),
		)
		resp := &tunnel.Response{Error: err.Error()}
		_ = tunnel.WriteResponse(stream, resp)
		return
	}

	session := a.getOrCreateSession(ctx, req.SessionID)
	mcpCtx := a.mcpServer.WithContext(ctx, session)

	result := a.mcpServer.HandleMessage(mcpCtx, req.Payload)

	respPayload, err := json.Marshal(result)
	if err != nil {
		resp := &tunnel.Response{Error: fmt.Sprintf("marshal response: %v", err)}
		_ = tunnel.WriteResponse(stream, resp)
		return
	}

	resp := &tunnel.Response{Payload: respPayload}
	if err := tunnel.WriteResponse(stream, resp); err != nil {
		a.logger.Error("failed to write tunnel response",
			slog.String("error", err.Error()),
		)
	}
}

func (a *Agent) getOrCreateSession(ctx context.Context, sessionID string) *server.InProcessSession {
	a.mu.Lock()
	defer a.mu.Unlock()

	if s, ok := a.sessions[sessionID]; ok {
		return s
	}

	s := server.NewInProcessSession(sessionID, nil)
	a.sessions[sessionID] = s
	if err := a.mcpServer.RegisterSession(ctx, s); err != nil {
		a.logger.Warn("failed to register session",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
	}

	return s
}
