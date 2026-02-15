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

	"github.com/coder/websocket"
	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
	"github.com/hashicorp/yamux"
)

// ErrNoAgent is returned when a call is forwarded but no agent is connected.
var ErrNoAgent = errors.New("no agent connected")

// TunnelServer manages the WebSocket connection to the agent and forwards
// MCP calls through the yamux tunnel.
type TunnelServer struct {
	logger  *slog.Logger
	apiKeys map[string]bool

	mu           sync.RWMutex
	yamuxSession *yamux.Session

	onConnect    func()
	onDisconnect func()
}

// NewTunnelServer creates a new tunnel server with the given API keys.
func NewTunnelServer(apiKeys []string, logger *slog.Logger) *TunnelServer {
	keySet := make(map[string]bool, len(apiKeys))
	for _, k := range apiKeys {
		keySet[k] = true
	}
	return &TunnelServer{
		logger:  logger,
		apiKeys: keySet,
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
	defer wsConn.CloseNow()

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
	defer session.Close()

	s.mu.Lock()
	s.yamuxSession = session
	s.mu.Unlock()

	s.logger.Info("agent connected")

	if s.onConnect != nil {
		s.onConnect()
	}

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
	defer stream.Close()

	req := tunnel.Request{
		SessionID: sessionID,
		Payload:   payload,
	}
	if err := tunnel.WriteMsg(stream, &req); err != nil {
		return nil, fmt.Errorf("write tunnel request: %w", err)
	}

	var resp tunnel.Response
	if err := tunnel.ReadMsg(stream, &resp); err != nil {
		return nil, fmt.Errorf("read tunnel response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}

	return resp.Payload, nil
}

func (s *TunnelServer) authenticate(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	return s.apiKeys[token]
}
