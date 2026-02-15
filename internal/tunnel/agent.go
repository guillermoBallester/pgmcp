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
	"time"

	"github.com/coder/websocket"
	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/server"
)

// Agent connects to the cloud server via WebSocket and serves MCP calls
// received through yamux streams.
type Agent struct {
	tunnelURL string
	apiKey    string
	mcpServer *server.MCPServer
	logger    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*server.InProcessSession
}

// NewAgent creates a new tunnel agent.
func NewAgent(tunnelURL, apiKey string, mcpServer *server.MCPServer, logger *slog.Logger) *Agent {
	return &Agent{
		tunnelURL: tunnelURL,
		apiKey:    apiKey,
		mcpServer: mcpServer,
		logger:    logger,
		sessions:  make(map[string]*server.InProcessSession),
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

func (a *Agent) connectAndServe(ctx context.Context) error {
	a.logger.Info("connecting to tunnel server",
		slog.String("url", a.tunnelURL),
	)

	wsConn, _, err := websocket.Dial(ctx, a.tunnelURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + a.apiKey},
		},
	})
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	defer wsConn.CloseNow()

	// Convert WebSocket to a net.Conn for yamux.
	netConn := websocket.NetConn(ctx, wsConn, websocket.MessageBinary)

	// Agent is yamux SERVER — the cloud server opens streams to us.
	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.LogOutput = io.Discard
	session, err := yamux.Server(netConn, yamuxCfg)
	if err != nil {
		return fmt.Errorf("yamux server: %w", err)
	}
	defer session.Close()

	a.logger.Info("tunnel connected")

	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("yamux accept: %w", err)
		}
		go a.handleStream(ctx, stream)
	}
}

func (a *Agent) handleStream(ctx context.Context, stream net.Conn) {
	defer stream.Close()

	var req tunnel.Request
	if err := tunnel.ReadMsg(stream, &req); err != nil {
		a.logger.Error("failed to read tunnel request",
			slog.String("error", err.Error()),
		)
		return
	}

	session := a.getOrCreateSession(ctx, req.SessionID)
	mcpCtx := a.mcpServer.WithContext(ctx, session)

	result := a.mcpServer.HandleMessage(mcpCtx, req.Payload)

	respPayload, err := json.Marshal(result)
	if err != nil {
		resp := tunnel.Response{Error: fmt.Sprintf("marshal response: %v", err)}
		_ = tunnel.WriteMsg(stream, &resp)
		return
	}

	resp := tunnel.Response{Payload: respPayload}
	if err := tunnel.WriteMsg(stream, &resp); err != nil {
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
