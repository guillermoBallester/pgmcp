package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadRequestRoundTrip(t *testing.T) {
	req := &Request{
		SessionID: "sess-123",
		Payload:   json.RawMessage(`{"method":"tools/list"}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteRequest(&buf, req))

	got, err := ReadRequest(&buf)
	require.NoError(t, err)

	assert.Equal(t, req.SessionID, got.SessionID)
	assert.JSONEq(t, string(req.Payload), string(got.Payload))
}

func TestWriteReadResponseRoundTrip(t *testing.T) {
	resp := &Response{
		Payload: json.RawMessage(`{"result":"ok"}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteResponse(&buf, resp))

	got, err := ReadResponse(&buf)
	require.NoError(t, err)

	assert.JSONEq(t, string(resp.Payload), string(got.Payload))
	assert.Empty(t, got.Error)
}

func TestWriteReadResponseError(t *testing.T) {
	resp := &Response{
		Error: "connection refused",
	}

	var buf bytes.Buffer
	require.NoError(t, WriteResponse(&buf, resp))

	got, err := ReadResponse(&buf)
	require.NoError(t, err)

	assert.Equal(t, "connection refused", got.Error)
	assert.Nil(t, got.Payload)
}

func TestMagicByteCorruption(t *testing.T) {
	req := &Request{
		SessionID: "sess-1",
		Payload:   json.RawMessage(`{}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteRequest(&buf, req))

	// Corrupt the first magic byte.
	data := buf.Bytes()
	data[0] = 0xFF

	_, err := ReadRequest(bytes.NewReader(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid magic bytes")
}

func TestVersionMismatch(t *testing.T) {
	req := &Request{
		SessionID: "sess-1",
		Payload:   json.RawMessage(`{}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteRequest(&buf, req))

	// Set version to 99.
	data := buf.Bytes()
	data[2] = 99

	_, err := ReadRequest(bytes.NewReader(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol version")
}

func TestMessageTypeMismatch(t *testing.T) {
	// Write a Request frame, try to read it as a Response.
	req := &Request{
		SessionID: "sess-1",
		Payload:   json.RawMessage(`{}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteRequest(&buf, req))

	_, err := ReadResponse(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected message type")
}

func TestRequestValidateEmptySessionID(t *testing.T) {
	req := &Request{
		SessionID: "",
		Payload:   json.RawMessage(`{}`),
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sessionId is required")
}

func TestRequestValidateInvalidJSON(t *testing.T) {
	req := &Request{
		SessionID: "sess-1",
		Payload:   json.RawMessage(`{not json`),
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload is not valid JSON")
}

func TestRequestValidateNilPayload(t *testing.T) {
	req := &Request{
		SessionID: "sess-1",
		Payload:   nil,
	}

	// nil payload is allowed (omitted).
	require.NoError(t, req.Validate())
}

func TestResponseValidateBothPayloadAndError(t *testing.T) {
	resp := &Response{
		Payload: json.RawMessage(`{"x":1}`),
		Error:   "something broke",
	}

	err := resp.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestResponseValidateNeitherPayloadNorError(t *testing.T) {
	resp := &Response{}

	err := resp.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one of payload or error is required")
}

func TestWriteRequestValidationFailure(t *testing.T) {
	req := &Request{SessionID: "", Payload: json.RawMessage(`{}`)}

	var buf bytes.Buffer
	err := WriteRequest(&buf, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sessionId is required")
	assert.Equal(t, 0, buf.Len(), "nothing should be written on validation failure")
}

func TestWriteResponseValidationFailure(t *testing.T) {
	resp := &Response{
		Payload: json.RawMessage(`{}`),
		Error:   "oops",
	}

	var buf bytes.Buffer
	err := WriteResponse(&buf, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
	assert.Equal(t, 0, buf.Len())
}

func TestReadRequestOversized(t *testing.T) {
	// Manually craft a header with a size exceeding MaxMessageSize.
	var buf bytes.Buffer
	var header [headerSize]byte
	header[0] = MagicBytes[0]
	header[1] = MagicBytes[1]
	header[2] = ProtocolVersion
	header[3] = byte(MessageTypeRequest)
	binary.BigEndian.PutUint32(header[4:], MaxMessageSize+1)
	buf.Write(header[:])

	_, err := ReadRequest(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestWriteRequestOversized(t *testing.T) {
	big := make([]byte, MaxMessageSize+1)
	for i := range big {
		big[i] = 'A'
	}
	req := &Request{
		SessionID: "sess-big",
		Payload:   json.RawMessage(`"` + string(big) + `"`),
	}

	var buf bytes.Buffer
	err := WriteRequest(&buf, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestReadRequestTruncatedHeader(t *testing.T) {
	// Only 3 bytes instead of 8.
	buf := bytes.NewReader([]byte{0x50, 0x47, 0x01})

	_, err := ReadRequest(buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read header")
}

func TestReadRequestTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	var header [headerSize]byte
	header[0] = MagicBytes[0]
	header[1] = MagicBytes[1]
	header[2] = ProtocolVersion
	header[3] = byte(MessageTypeRequest)
	binary.BigEndian.PutUint32(header[4:], 100)
	buf.Write(header[:])
	buf.Write([]byte("hello"))

	_, err := ReadRequest(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read payload")
}

func TestMultipleMessagesOnSameBuffer(t *testing.T) {
	var buf bytes.Buffer

	reqs := []*Request{
		{SessionID: "s1", Payload: json.RawMessage(`{"id":1}`)},
		{SessionID: "s2", Payload: json.RawMessage(`{"id":2}`)},
		{SessionID: "s3", Payload: json.RawMessage(`{"id":3}`)},
	}

	for _, req := range reqs {
		require.NoError(t, WriteRequest(&buf, req))
	}

	for _, want := range reqs {
		got, err := ReadRequest(&buf)
		require.NoError(t, err)
		assert.Equal(t, want.SessionID, got.SessionID)
	}
}

func TestWriteReadPingRoundTrip(t *testing.T) {
	ping := &Ping{Timestamp: time.Now().UnixNano()}

	var buf bytes.Buffer
	require.NoError(t, WritePing(&buf, ping))

	got, err := ReadPing(&buf)
	require.NoError(t, err)
	assert.Equal(t, ping.Timestamp, got.Timestamp)
}

func TestWriteReadPongRoundTrip(t *testing.T) {
	pong := &Pong{Timestamp: time.Now().UnixNano()}

	var buf bytes.Buffer
	require.NoError(t, WritePong(&buf, pong))

	got, err := ReadPong(&buf)
	require.NoError(t, err)
	assert.Equal(t, pong.Timestamp, got.Timestamp)
	assert.False(t, got.Draining)
}

func TestPongWithDraining(t *testing.T) {
	pong := &Pong{Timestamp: time.Now().UnixNano(), Draining: true}

	var buf bytes.Buffer
	require.NoError(t, WritePong(&buf, pong))

	got, err := ReadPong(&buf)
	require.NoError(t, err)
	assert.Equal(t, pong.Timestamp, got.Timestamp)
	assert.True(t, got.Draining)
}

func TestPingValidateZeroTimestamp(t *testing.T) {
	ping := &Ping{Timestamp: 0}
	err := ping.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp must be positive")

	// Also verify WritePing rejects it.
	var buf bytes.Buffer
	err = WritePing(&buf, ping)
	require.Error(t, err)
	assert.Equal(t, 0, buf.Len())
}

func TestReadRawFrameDispatch(t *testing.T) {
	var buf bytes.Buffer

	// Write a Ping then a Request to the same buffer.
	ping := &Ping{Timestamp: 12345}
	require.NoError(t, WritePing(&buf, ping))

	req := &Request{SessionID: "s1", Payload: json.RawMessage(`{"id":1}`)}
	require.NoError(t, WriteRequest(&buf, req))

	// Read the Ping via ReadRawFrame.
	msgType, payload, err := ReadRawFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, MessageTypePing, msgType)

	var gotPing Ping
	require.NoError(t, json.Unmarshal(payload, &gotPing))
	assert.Equal(t, int64(12345), gotPing.Timestamp)

	// Read the Request via ReadRawFrame.
	msgType, payload, err = ReadRawFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRequest, msgType)

	var gotReq Request
	require.NoError(t, json.Unmarshal(payload, &gotReq))
	assert.Equal(t, "s1", gotReq.SessionID)
}

func TestReadRawFrameRejectsCorruption(t *testing.T) {
	// Write valid ping, then corrupt magic bytes.
	ping := &Ping{Timestamp: 1}
	var buf bytes.Buffer
	require.NoError(t, WritePing(&buf, ping))

	data := buf.Bytes()
	data[0] = 0xFF

	_, _, err := ReadRawFrame(bytes.NewReader(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid magic bytes")
}

func TestWriteReadHandshakeRoundTrip(t *testing.T) {
	h := &Handshake{
		ProtocolVersion: 1,
		ServerVersion:   "0.1.0",
		Features:        []string{"column-masking", "table-policies"},
	}

	var buf bytes.Buffer
	require.NoError(t, WriteHandshake(&buf, h))

	got, err := ReadHandshake(&buf)
	require.NoError(t, err)
	assert.Equal(t, h.ProtocolVersion, got.ProtocolVersion)
	assert.Equal(t, h.ServerVersion, got.ServerVersion)
	assert.Equal(t, h.Features, got.Features)
}

func TestWriteReadHandshakeAckRoundTrip(t *testing.T) {
	ack := &HandshakeAck{
		ProtocolVersion: 1,
		AgentVersion:    "0.1.0",
		Features:        []string{"streaming-responses"},
	}

	var buf bytes.Buffer
	require.NoError(t, WriteHandshakeAck(&buf, ack))

	got, err := ReadHandshakeAck(&buf)
	require.NoError(t, err)
	assert.Equal(t, ack.ProtocolVersion, got.ProtocolVersion)
	assert.Equal(t, ack.AgentVersion, got.AgentVersion)
	assert.Equal(t, ack.Features, got.Features)
	assert.Empty(t, got.Error)
}

func TestHandshakeAckWithError(t *testing.T) {
	ack := &HandshakeAck{
		ProtocolVersion: 1,
		AgentVersion:    "0.1.0",
		Error:           "incompatible protocol version",
	}

	var buf bytes.Buffer
	require.NoError(t, WriteHandshakeAck(&buf, ack))

	got, err := ReadHandshakeAck(&buf)
	require.NoError(t, err)
	assert.Equal(t, "incompatible protocol version", got.Error)
}

func TestHandshakeValidation(t *testing.T) {
	// Zero protocol version.
	h := &Handshake{ProtocolVersion: 0, ServerVersion: "0.1.0"}
	err := h.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protocolVersion must be positive")

	// Empty server version.
	h = &Handshake{ProtocolVersion: 1, ServerVersion: ""}
	err = h.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serverVersion is required")

	// HandshakeAck: zero protocol version.
	ack := &HandshakeAck{ProtocolVersion: 0, AgentVersion: "0.1.0"}
	err = ack.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protocolVersion must be positive")

	// HandshakeAck: empty agent version.
	ack = &HandshakeAck{ProtocolVersion: 1, AgentVersion: ""}
	err = ack.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agentVersion is required")
}

func TestWriteHandshakeValidationFailure(t *testing.T) {
	h := &Handshake{ProtocolVersion: 0, ServerVersion: "0.1.0"}
	var buf bytes.Buffer
	err := WriteHandshake(&buf, h)
	require.Error(t, err)
	assert.Equal(t, 0, buf.Len())
}

func TestHeaderLayout(t *testing.T) {
	req := &Request{
		SessionID: "s1",
		Payload:   json.RawMessage(`{}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteRequest(&buf, req))

	data := buf.Bytes()
	require.True(t, len(data) >= headerSize)

	// Verify header fields.
	assert.Equal(t, byte(0x50), data[0], "magic byte 0")
	assert.Equal(t, byte(0x47), data[1], "magic byte 1")
	assert.Equal(t, byte(ProtocolVersion), data[2], "version")
	assert.Equal(t, byte(MessageTypeRequest), data[3], "message type")

	payloadLen := binary.BigEndian.Uint32(data[4:8])
	assert.Equal(t, uint32(len(data)-headerSize), payloadLen, "payload length")
}
