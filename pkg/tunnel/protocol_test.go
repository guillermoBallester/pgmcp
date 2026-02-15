package tunnel

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadMsgRoundTrip(t *testing.T) {
	req := Request{
		SessionID: "sess-123",
		Payload:   json.RawMessage(`{"method":"tools/list"}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteMsg(&buf, &req))

	var got Request
	require.NoError(t, ReadMsg(&buf, &got))

	assert.Equal(t, req.SessionID, got.SessionID)
	assert.JSONEq(t, string(req.Payload), string(got.Payload))
}

func TestWriteReadMsgResponse(t *testing.T) {
	resp := Response{
		Payload: json.RawMessage(`{"result":{"content":[{"type":"text","text":"hello"}]}}`),
	}

	var buf bytes.Buffer
	require.NoError(t, WriteMsg(&buf, &resp))

	var got Response
	require.NoError(t, ReadMsg(&buf, &got))

	assert.JSONEq(t, string(resp.Payload), string(got.Payload))
	assert.Empty(t, got.Error)
}

func TestWriteReadMsgResponseError(t *testing.T) {
	resp := Response{
		Error: "connection refused",
	}

	var buf bytes.Buffer
	require.NoError(t, WriteMsg(&buf, &resp))

	var got Response
	require.NoError(t, ReadMsg(&buf, &got))

	assert.Equal(t, "connection refused", got.Error)
	assert.Nil(t, got.Payload)
}

func TestReadMsgOversized(t *testing.T) {
	// Write a length prefix that exceeds MaxMessageSize.
	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], MaxMessageSize+1)
	buf.Write(lenBuf[:])

	var got Request
	err := ReadMsg(&buf, &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestWriteMsgOversized(t *testing.T) {
	// Create a payload that exceeds MaxMessageSize.
	big := make([]byte, MaxMessageSize+1)
	for i := range big {
		big[i] = 'A'
	}
	req := Request{
		SessionID: "sess-big",
		Payload:   json.RawMessage(`"` + string(big) + `"`),
	}

	var buf bytes.Buffer
	err := WriteMsg(&buf, &req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestWriteReadMsgEmptyPayload(t *testing.T) {
	req := Request{
		SessionID: "sess-empty",
		Payload:   nil,
	}

	var buf bytes.Buffer
	require.NoError(t, WriteMsg(&buf, &req))

	var got Request
	require.NoError(t, ReadMsg(&buf, &got))

	assert.Equal(t, "sess-empty", got.SessionID)
	// JSON marshals nil as "null", which round-trips to json.RawMessage("null").
	assert.JSONEq(t, "null", string(got.Payload))
}

func TestReadMsgTruncatedLength(t *testing.T) {
	// Only 2 bytes instead of 4 — should fail.
	buf := bytes.NewReader([]byte{0x00, 0x01})

	var got Request
	err := ReadMsg(buf, &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read length prefix")
}

func TestReadMsgTruncatedPayload(t *testing.T) {
	// Length says 100 bytes but only 5 bytes follow.
	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], 100)
	buf.Write(lenBuf[:])
	buf.Write([]byte("hello"))

	var got Request
	err := ReadMsg(&buf, &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read payload")
}

func TestMultipleMessagesOnSameBuffer(t *testing.T) {
	var buf bytes.Buffer

	msgs := []Request{
		{SessionID: "s1", Payload: json.RawMessage(`{"id":1}`)},
		{SessionID: "s2", Payload: json.RawMessage(`{"id":2}`)},
		{SessionID: "s3", Payload: json.RawMessage(`{"id":3}`)},
	}

	for i := range msgs {
		require.NoError(t, WriteMsg(&buf, &msgs[i]))
	}

	for _, want := range msgs {
		var got Request
		require.NoError(t, ReadMsg(&buf, &got))
		assert.Equal(t, want.SessionID, got.SessionID)
	}
}
