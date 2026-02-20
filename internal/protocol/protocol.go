package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MagicBytes identifies a valid tunnel protocol frame ("PG").
var MagicBytes = [2]byte{0x50, 0x47}

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion uint8 = 1

// MaxMessageSize is the maximum allowed message size (16 MB).
const MaxMessageSize = 16 * 1024 * 1024

// headerSize is the total frame header: 2 magic + 1 version + 1 type + 4 length.
const headerSize = 8

// MessageType discriminates frame types on the wire.
type MessageType uint8

const (
	// MessageTypeRequest is sent from the cloud server to the agent.
	MessageTypeRequest MessageType = 1
	// MessageTypeResponse is sent from the agent back to the cloud server.
	MessageTypeResponse MessageType = 2
	// MessageTypePing is sent from the cloud server to the agent for heartbeat.
	MessageTypePing MessageType = 3
	// MessageTypePong is sent from the agent back to the cloud server in response to a ping.
	MessageTypePong MessageType = 4
	// MessageTypeHandshake is sent from the server to the agent after yamux is established.
	MessageTypeHandshake MessageType = 5
	// MessageTypeHandshakeAck is sent from the agent back to the server in response to a handshake.
	MessageTypeHandshakeAck MessageType = 6
)

// Request is the envelope sent from the cloud server to the agent through a yamux stream.
type Request struct {
	SessionID string          `json:"sessionId"`
	Payload   json.RawMessage `json:"payload"`
}

// Validate checks that the request is well-formed.
func (r *Request) Validate() error {
	if r.SessionID == "" {
		return fmt.Errorf("request validation: sessionId is required")
	}
	if len(r.Payload) > 0 && !json.Valid(r.Payload) {
		return fmt.Errorf("request validation: payload is not valid JSON")
	}
	return nil
}

// Response is the envelope sent from the agent back to the cloud server through a yamux stream.
type Response struct {
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Validate checks that the response is well-formed.
func (r *Response) Validate() error {
	if len(r.Payload) > 0 && r.Error != "" {
		return fmt.Errorf("response validation: payload and error are mutually exclusive")
	}
	if len(r.Payload) == 0 && r.Error == "" {
		return fmt.Errorf("response validation: one of payload or error is required")
	}
	if len(r.Payload) > 0 && !json.Valid(r.Payload) {
		return fmt.Errorf("response validation: payload is not valid JSON")
	}
	return nil
}

// Ping is the heartbeat message sent from the cloud server to the agent.
type Ping struct {
	Timestamp int64 `json:"timestamp"` // Unix nanos
}

// Validate checks that the ping is well-formed.
func (p *Ping) Validate() error {
	if p.Timestamp <= 0 {
		return fmt.Errorf("ping validation: timestamp must be positive")
	}
	return nil
}

// Pong is the heartbeat response sent from the agent back to the cloud server.
type Pong struct {
	Timestamp int64 `json:"timestamp"`          // Echo back the ping timestamp
	Draining  bool  `json:"draining,omitempty"` // True when agent is shutting down
}

// Validate checks that the pong is well-formed.
func (p *Pong) Validate() error {
	if p.Timestamp <= 0 {
		return fmt.Errorf("pong validation: timestamp must be positive")
	}
	return nil
}

// WritePing marshals a Ping and writes it as a typed frame.
func WritePing(w io.Writer, p *Ping) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypePing, p)
}

// ReadPing reads and unmarshals a Ping frame, rejecting wrong message types.
func ReadPing(r io.Reader) (*Ping, error) {
	var p Ping
	if err := readMsg(r, MessageTypePing, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// WritePong marshals a Pong and writes it as a typed frame.
func WritePong(w io.Writer, p *Pong) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypePong, p)
}

// ReadPong reads and unmarshals a Pong frame, rejecting wrong message types.
func ReadPong(r io.Reader) (*Pong, error) {
	var p Pong
	if err := readMsg(r, MessageTypePong, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Handshake is sent from the server to the agent as the first exchange after yamux is established.
type Handshake struct {
	ProtocolVersion uint8    `json:"protocolVersion"`
	ServerVersion   string   `json:"serverVersion"`
	Features        []string `json:"features,omitempty"`
}

// Validate checks that the handshake is well-formed.
func (h *Handshake) Validate() error {
	if h.ProtocolVersion == 0 {
		return fmt.Errorf("handshake validation: protocolVersion must be positive")
	}
	if h.ServerVersion == "" {
		return fmt.Errorf("handshake validation: serverVersion is required")
	}
	return nil
}

// HandshakeAck is sent from the agent back to the server in response to a Handshake.
type HandshakeAck struct {
	ProtocolVersion uint8    `json:"protocolVersion"`
	AgentVersion    string   `json:"agentVersion"`
	Features        []string `json:"features,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// Validate checks that the handshake ack is well-formed.
func (a *HandshakeAck) Validate() error {
	if a.ProtocolVersion == 0 {
		return fmt.Errorf("handshake ack validation: protocolVersion must be positive")
	}
	if a.AgentVersion == "" {
		return fmt.Errorf("handshake ack validation: agentVersion is required")
	}
	return nil
}

// WriteHandshake marshals a Handshake and writes it as a typed frame.
func WriteHandshake(w io.Writer, h *Handshake) error {
	if err := h.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypeHandshake, h)
}

// ReadHandshake reads and unmarshals a Handshake frame, rejecting wrong message types.
func ReadHandshake(r io.Reader) (*Handshake, error) {
	var h Handshake
	if err := readMsg(r, MessageTypeHandshake, &h); err != nil {
		return nil, err
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return &h, nil
}

// WriteHandshakeAck marshals a HandshakeAck and writes it as a typed frame.
func WriteHandshakeAck(w io.Writer, a *HandshakeAck) error {
	if err := a.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypeHandshakeAck, a)
}

// ReadHandshakeAck reads and unmarshals a HandshakeAck frame, rejecting wrong message types.
func ReadHandshakeAck(r io.Reader) (*HandshakeAck, error) {
	var a HandshakeAck
	if err := readMsg(r, MessageTypeHandshakeAck, &a); err != nil {
		return nil, err
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// ReadRawFrame reads the 8-byte header, validates magic/version, and returns
// the message type and raw JSON payload for caller dispatch. Unlike readMsg,
// it does not check the message type or unmarshal the payload.
func ReadRawFrame(r io.Reader) (MessageType, json.RawMessage, error) {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}

	if header[0] != MagicBytes[0] || header[1] != MagicBytes[1] {
		return 0, nil, fmt.Errorf("invalid magic bytes: %#x %#x", header[0], header[1])
	}

	version := header[2]
	if version != ProtocolVersion {
		return 0, nil, fmt.Errorf("unsupported protocol version: %d", version)
	}

	msgType := MessageType(header[3])

	size := binary.BigEndian.Uint32(header[4:])
	if size > MaxMessageSize {
		return 0, nil, fmt.Errorf("message size %d exceeds maximum %d", size, MaxMessageSize)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, fmt.Errorf("read payload: %w", err)
	}

	return msgType, json.RawMessage(data), nil
}

// WriteRequest marshals a Request and writes it as a typed frame.
func WriteRequest(w io.Writer, req *Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypeRequest, req)
}

// ReadRequest reads and unmarshals a Request frame, rejecting wrong message types.
func ReadRequest(r io.Reader) (*Request, error) {
	var req Request
	if err := readMsg(r, MessageTypeRequest, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return &req, nil
}

// WriteResponse marshals a Response and writes it as a typed frame.
func WriteResponse(w io.Writer, resp *Response) error {
	if err := resp.Validate(); err != nil {
		return err
	}
	return writeMsg(w, MessageTypeResponse, resp)
}

// ReadResponse reads and unmarshals a Response frame, rejecting wrong message types.
func ReadResponse(r io.Reader) (*Response, error) {
	var resp Response
	if err := readMsg(r, MessageTypeResponse, &resp); err != nil {
		return nil, err
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}
	return &resp, nil
}

// writeMsg marshals v as JSON and writes it with the 8-byte header:
// [2-byte magic][1-byte version][1-byte msg type][4-byte big-endian length].
func writeMsg(w io.Writer, msgType MessageType, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if len(data) > MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), MaxMessageSize)
	}

	var header [headerSize]byte
	header[0] = MagicBytes[0]
	header[1] = MagicBytes[1]
	header[2] = ProtocolVersion
	header[3] = byte(msgType)
	binary.BigEndian.PutUint32(header[4:], uint32(len(data)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// readMsg reads the 8-byte header, validates magic/version/type, then reads
// and unmarshals the JSON payload into v.
func readMsg(r io.Reader, expectedType MessageType, v any) error {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	if header[0] != MagicBytes[0] || header[1] != MagicBytes[1] {
		return fmt.Errorf("invalid magic bytes: %#x %#x", header[0], header[1])
	}

	version := header[2]
	if version != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version: %d", version)
	}

	msgType := MessageType(header[3])
	if msgType != expectedType {
		return fmt.Errorf("unexpected message type: got %d, want %d", msgType, expectedType)
	}

	size := binary.BigEndian.Uint32(header[4:])
	if size > MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", size, MaxMessageSize)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	return nil
}
