package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/server"
)

// trackedSession wraps an InProcessSession with last-activity tracking
// to enable TTL-based eviction of idle sessions.
type trackedSession struct {
	session    *server.InProcessSession
	lastActive atomic.Int64 // UnixNano timestamp
}

func (ts *trackedSession) touch() {
	ts.lastActive.Store(time.Now().UnixNano())
}

// Agent connects to the cloud server via WebSocket and serves MCP calls
// received through yamux streams.
type Agent struct {
	tunnelURL    string
	apiKey       string
	agentVersion string
	mcpServer    *server.MCPServer
	logger       *slog.Logger
	cfg          tunnel.AgentTunnelConfig

	mu       sync.Mutex
	sessions map[string]*trackedSession

	drainMu  sync.Mutex     // protects draining check + wg.Add atomicity
	wg       sync.WaitGroup // tracks in-flight handleStream goroutines
	draining atomic.Bool    // true when shutdown initiated
}

// NewAgent creates a new tunnel agent.
func NewAgent(tunnelURL, apiKey, agentVersion string, mcpServer *server.MCPServer, cfg tunnel.AgentTunnelConfig, logger *slog.Logger) *Agent {
	return &Agent{
		tunnelURL:    tunnelURL,
		apiKey:       apiKey,
		agentVersion: agentVersion,
		mcpServer:    mcpServer,
		logger:       logger,
		sessions:     make(map[string]*trackedSession),
		cfg:          cfg,
	}
}

// Run connects to the cloud server and serves MCP calls. It reconnects
// with exponential backoff on failure. Returns when ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	backoff := a.cfg.InitialBackoff

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

		backoff = min(backoff*2, a.cfg.MaxBackoff)
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

// connectAndServe establishes a tunnel connection and serves until disconnected.
func (a *Agent) connectAndServe(ctx context.Context) error {
	session, connCtx, connCancel, err := a.dial(ctx)
	if err != nil {
		return err
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup
	defer connCancel()

	a.logger.Info("tunnel connected")

	// Clear stale sessions from a previous connection.
	a.clearSessions(ctx)

	// Start periodic cleanup of idle sessions.
	cleanupCancel := a.runSessionCleanup(connCtx)

	// When the parent context is cancelled, drain in-flight handlers then
	// close the session.
	a.watchForShutdown(ctx, session, connCancel, cleanupCancel)

	return a.acceptLoop(ctx, connCtx, session)
}

// dial establishes the WebSocket + yamux connection. Returns the yamux session,
// the connection-scoped context, and a cancel func for that context.
func (a *Agent) dial(ctx context.Context) (*yamux.Session, context.Context, context.CancelFunc, error) {
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
		return nil, nil, nil, fmt.Errorf("websocket dial: %w", err)
	}

	// Use a separate context for the connection lifetime so that cancelling
	// the parent ctx doesn't immediately tear down the WebSocket. This allows
	// in-flight handlers to finish during graceful shutdown.
	connCtx, connCancel := context.WithCancel(context.Background())

	netConn := websocket.NetConn(connCtx, wsConn, websocket.MessageBinary)

	// Agent is yamux SERVER — the cloud server opens streams to us.
	session, err := yamux.Server(netConn, tunnel.NewYamuxConfig(a.cfg.Yamux))
	if err != nil {
		connCancel()
		wsConn.CloseNow() //nolint:errcheck
		return nil, nil, nil, fmt.Errorf("yamux server: %w", err)
	}

	return session, connCtx, connCancel, nil
}

// runSessionCleanup starts a goroutine that periodically evicts idle sessions.
// Returns a cancel func to stop it.
func (a *Agent) runSessionCleanup(ctx context.Context) context.CancelFunc {
	cleanupCtx, cleanupCancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(a.cfg.SessionCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				a.cleanStaleSessions(cleanupCtx)
			}
		}
	}()
	return cleanupCancel
}

// watchForShutdown starts a goroutine that drains in-flight handlers when ctx
// is cancelled, then cleans up the session.
func (a *Agent) watchForShutdown(ctx context.Context, session *yamux.Session,
	connCancel, cleanupCancel context.CancelFunc) {
	go func() {
		<-ctx.Done()
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
			// All handlers drained gracefully.
		case <-time.After(a.cfg.ForceCloseTimeout):
			a.logger.Warn("force-closing tunnel: handlers did not drain in time")
		}

		a.clearSessions(context.Background())
		cleanupCancel()
		session.Close() //nolint:errcheck
		connCancel()
	}()
}

// acceptLoop accepts yamux streams and dispatches them to handleStream.
// Returns when the session is closed or ctx is cancelled.
func (a *Agent) acceptLoop(ctx, connCtx context.Context, session *yamux.Session) error {
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

func (a *Agent) getOrCreateSession(ctx context.Context, sessionID string) *server.InProcessSession {
	a.mu.Lock()
	if ts, ok := a.sessions[sessionID]; ok {
		ts.touch()
		a.mu.Unlock()
		return ts.session
	}

	s := server.NewInProcessSession(sessionID, nil)
	ts := &trackedSession{session: s}
	ts.touch()
	a.sessions[sessionID] = ts
	a.mu.Unlock()

	// Register outside the lock — RegisterSession may call back into
	// the MCP server which could acquire its own locks.
	if err := a.mcpServer.RegisterSession(ctx, s); err != nil {
		a.logger.Warn("failed to register session",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
	}

	return s
}

// clearSessions unregisters and removes all tracked sessions.
func (a *Agent) clearSessions(ctx context.Context) {
	a.mu.Lock()
	old := a.sessions
	a.sessions = make(map[string]*trackedSession)
	a.mu.Unlock()

	for id := range old {
		a.mcpServer.UnregisterSession(ctx, id)
	}
}

// cleanStaleSessions removes sessions that have been idle longer than the TTL.
func (a *Agent) cleanStaleSessions(ctx context.Context) {
	a.mu.Lock()
	now := time.Now().UnixNano()
	var stale []string
	for id, ts := range a.sessions {
		if now-ts.lastActive.Load() > int64(a.cfg.SessionTTL) {
			stale = append(stale, id)
			delete(a.sessions, id)
		}
	}
	a.mu.Unlock()

	for _, id := range stale {
		a.mcpServer.UnregisterSession(ctx, id)
		a.logger.Debug("evicted stale session",
			slog.String("session_id", id),
		)
	}
}
