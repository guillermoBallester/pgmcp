package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"github.com/guillermoBallester/isthmus/internal/protocol"
)

func (a *Agent) handleStream(ctx context.Context, stream net.Conn) {
	defer stream.Close() //nolint:errcheck // best-effort cleanup

	msgType, payload, err := protocol.ReadRawFrame(stream)
	if err != nil {
		a.logger.Error("failed to read tunnel frame",
			slog.String("error", err.Error()),
		)
		return
	}

	switch msgType {
	case protocol.MessageTypeHandshake:
		a.handleHandshake(stream, payload)
	case protocol.MessageTypePing:
		a.handlePing(stream, payload)
	case protocol.MessageTypeRequest:
		a.handleRequest(ctx, stream, payload)
	default:
		a.logger.Warn("unknown message type",
			slog.Int("type", int(msgType)),
		)
	}
}

func (a *Agent) handleHandshake(stream net.Conn, payload json.RawMessage) {
	var h protocol.Handshake
	if err := json.Unmarshal(payload, &h); err != nil {
		a.logger.Error("failed to unmarshal handshake",
			slog.String("error", err.Error()),
		)
		return
	}

	ack := &protocol.HandshakeAck{
		ProtocolVersion: protocol.ProtocolVersion,
		AgentVersion:    a.agentVersion,
	}

	// Check protocol version compatibility (exact match for now).
	if h.ProtocolVersion != protocol.ProtocolVersion {
		ack.Error = fmt.Sprintf("incompatible protocol version: server=%d, agent=%d", h.ProtocolVersion, protocol.ProtocolVersion)
		a.logger.Error("handshake version mismatch",
			slog.Uint64("server_version", uint64(h.ProtocolVersion)),
			slog.Uint64("agent_version", uint64(protocol.ProtocolVersion)),
		)
	} else {
		a.logger.Info("handshake received",
			slog.Uint64("protocol_version", uint64(h.ProtocolVersion)),
			slog.String("server_version", h.ServerVersion),
		)
	}

	if err := protocol.WriteHandshakeAck(stream, ack); err != nil {
		a.logger.Error("failed to write handshake ack",
			slog.String("error", err.Error()),
		)
	}
}

func (a *Agent) handlePing(stream net.Conn, payload json.RawMessage) {
	var ping protocol.Ping
	if err := json.Unmarshal(payload, &ping); err != nil {
		a.logger.Error("failed to unmarshal ping",
			slog.String("error", err.Error()),
		)
		return
	}

	pong := &protocol.Pong{
		Timestamp: ping.Timestamp,
		Draining:  a.draining.Load(),
	}

	if err := protocol.WritePong(stream, pong); err != nil {
		a.logger.Error("failed to write pong",
			slog.String("error", err.Error()),
		)
	}
}

func (a *Agent) handleRequest(ctx context.Context, stream net.Conn, payload json.RawMessage) {
	var req protocol.Request
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
		resp := &protocol.Response{Error: err.Error()}
		_ = protocol.WriteResponse(stream, resp)
		return
	}

	session := a.getOrCreateSession(ctx, req.SessionID)
	mcpCtx := a.mcpServer.WithContext(ctx, session)

	result := a.mcpServer.HandleMessage(mcpCtx, req.Payload)

	respPayload, err := json.Marshal(result)
	if err != nil {
		resp := &protocol.Response{Error: fmt.Sprintf("marshal response: %v", err)}
		_ = protocol.WriteResponse(stream, resp)
		return
	}

	resp := &protocol.Response{Payload: respPayload}
	if err := protocol.WriteResponse(stream, resp); err != nil {
		a.logger.Error("failed to write tunnel response",
			slog.String("error", err.Error()),
		)
	}
}
