package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MaxMessageSize is the maximum allowed message size (16 MB).
const MaxMessageSize = 16 * 1024 * 1024

// Request is the envelope sent from the cloud server to the agent through a yamux stream.
type Request struct {
	SessionID string          `json:"sessionId"`
	Payload   json.RawMessage `json:"payload"`
}

// Response is the envelope sent from the agent back to the cloud server through a yamux stream.
type Response struct {
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// WriteMsg marshals v as JSON and writes it to w with a 4-byte big-endian length prefix.
func WriteMsg(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if len(data) > MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), MaxMessageSize)
	}

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("write length prefix: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// ReadMsg reads a length-prefixed JSON message from r and unmarshals it into v.
func ReadMsg(r io.Reader, v any) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return fmt.Errorf("read length prefix: %w", err)
	}

	size := binary.BigEndian.Uint32(lenBuf[:])
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
