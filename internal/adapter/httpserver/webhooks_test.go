package httpserver

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Svix uses a whsec_ prefixed base64-encoded secret.
const testWebhookSecret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"

func newTestWebhookHandler(t *testing.T) *WebhookHandler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Create handler without pool/queries — we test signature verification,
	// not DB operations (those need integration tests).
	h := NewWebhookHandler(nil, nil, testWebhookSecret, logger)
	require.NotNil(t, h)
	return h
}

// signPayload creates valid Svix signature headers for a given body.
func signPayload(t *testing.T, msgID string, body []byte) http.Header {
	t.Helper()
	// Decode the secret (strip "whsec_" prefix, base64 decode).
	secretBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(testWebhookSecret, "whsec_"))
	require.NoError(t, err)

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	toSign := fmt.Sprintf("%s.%s.%s", msgID, timestamp, string(body))

	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(toSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return http.Header{
		"svix-id":        {msgID},
		"svix-timestamp": {timestamp},
		"svix-signature": {"v1," + sig},
	}
}

func TestWebhook_InvalidSignature(t *testing.T) {
	h := newTestWebhookHandler(t)
	handler := h.HandleClerkWebhook()

	body := []byte(`{"type":"user.created","data":{}}`)
	req := httptest.NewRequest("POST", "/api/webhooks/clerk", bytes.NewReader(body))
	req.Header.Set("svix-id", "msg_123")
	req.Header.Set("svix-timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("svix-signature", "v1,invalidsignature")

	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid signature")
}

func TestWebhook_MissingHeaders(t *testing.T) {
	h := newTestWebhookHandler(t)
	handler := h.HandleClerkWebhook()

	body := []byte(`{"type":"user.created","data":{}}`)
	req := httptest.NewRequest("POST", "/api/webhooks/clerk", bytes.NewReader(body))
	// No svix headers at all.

	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestWebhook_ValidSignature_UnhandledEvent(t *testing.T) {
	h := newTestWebhookHandler(t)
	handler := h.HandleClerkWebhook()

	event := map[string]any{
		"type": "some.unknown.event",
		"data": map[string]any{},
	}
	body, _ := json.Marshal(event)

	headers := signPayload(t, "msg_test1", body)
	req := httptest.NewRequest("POST", "/api/webhooks/clerk", bytes.NewReader(body))
	for k, v := range headers {
		for _, vv := range v {
			req.Header.Set(k, vv)
		}
	}

	w := httptest.NewRecorder()
	handler(w, req)

	// Unhandled events should return 200 (accepted, ignored).
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWebhook_InvalidJSON(t *testing.T) {
	h := newTestWebhookHandler(t)
	handler := h.HandleClerkWebhook()

	body := []byte(`not valid json`)
	headers := signPayload(t, "msg_test2", body)
	req := httptest.NewRequest("POST", "/api/webhooks/clerk", bytes.NewReader(body))
	for k, v := range headers {
		for _, vv := range v {
			req.Header.Set(k, vv)
		}
	}

	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid json")
}

func TestWebhook_BodySizeLimit(t *testing.T) {
	h := newTestWebhookHandler(t)
	handler := h.HandleClerkWebhook()

	// Create a body larger than 1 MB.
	bigBody := bytes.Repeat([]byte("x"), (1<<20)+100)
	req := httptest.NewRequest("POST", "/api/webhooks/clerk", bytes.NewReader(bigBody))
	req.Header.Set("svix-id", "msg_big")
	req.Header.Set("svix-timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("svix-signature", "v1,fake")

	w := httptest.NewRecorder()
	handler(w, req)

	// The truncated body won't have a valid signature → 401.
	// The important thing is the server doesn't OOM.
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestNewWebhookHandler_InvalidSecret(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewWebhookHandler(nil, nil, "not-a-valid-secret", logger)
	assert.Nil(t, h)
}
