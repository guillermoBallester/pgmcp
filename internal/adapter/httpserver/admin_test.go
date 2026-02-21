package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	"github.com/guillermoBallester/isthmus/internal/protocol"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAdminSecret = "test-admin-secret"

// --- mock AdminRepository ---

type mockAdminRepo struct {
	createAPIKeyFn   func(ctx context.Context, wsID, dbID uuid.UUID, keyHash, keyPrefix, name string) (*port.APIKeyRecord, error)
	listAPIKeysFn    func(ctx context.Context, wsID uuid.UUID) ([]port.APIKeyRecord, error)
	deleteAPIKeyFn   func(ctx context.Context, id, wsID uuid.UUID) error
	createDatabaseFn func(ctx context.Context, wsID uuid.UUID, name, connType, status string, encURL []byte) (*port.DatabaseInfo, error)
	listDatabasesFn  func(ctx context.Context, wsID uuid.UUID) ([]port.DatabaseInfo, error)
	deleteDatabaseFn func(ctx context.Context, id, wsID uuid.UUID) error
}

func (m *mockAdminRepo) CreateAPIKey(ctx context.Context, wsID, dbID uuid.UUID, keyHash, keyPrefix, name string) (*port.APIKeyRecord, error) {
	if m.createAPIKeyFn != nil {
		return m.createAPIKeyFn(ctx, wsID, dbID, keyHash, keyPrefix, name)
	}
	return &port.APIKeyRecord{
		ID:          uuid.New(),
		KeyPrefix:   keyPrefix,
		Name:        name,
		WorkspaceID: wsID,
		DatabaseID:  dbID,
		CreatedAt:   time.Now(),
	}, nil
}

func (m *mockAdminRepo) ListAPIKeys(ctx context.Context, wsID uuid.UUID) ([]port.APIKeyRecord, error) {
	if m.listAPIKeysFn != nil {
		return m.listAPIKeysFn(ctx, wsID)
	}
	return nil, nil
}

func (m *mockAdminRepo) DeleteAPIKey(ctx context.Context, id, wsID uuid.UUID) error {
	if m.deleteAPIKeyFn != nil {
		return m.deleteAPIKeyFn(ctx, id, wsID)
	}
	return nil
}

func (m *mockAdminRepo) CreateDatabase(ctx context.Context, wsID uuid.UUID, name, connType, status string, encURL []byte) (*port.DatabaseInfo, error) {
	if m.createDatabaseFn != nil {
		return m.createDatabaseFn(ctx, wsID, name, connType, status, encURL)
	}
	return &port.DatabaseInfo{
		ID:             uuid.New(),
		WorkspaceID:    wsID,
		Name:           name,
		ConnectionType: connType,
		Status:         status,
		CreatedAt:      time.Now(),
	}, nil
}

func (m *mockAdminRepo) ListDatabases(ctx context.Context, wsID uuid.UUID) ([]port.DatabaseInfo, error) {
	if m.listDatabasesFn != nil {
		return m.listDatabasesFn(ctx, wsID)
	}
	return nil, nil
}

func (m *mockAdminRepo) DeleteDatabase(ctx context.Context, id, wsID uuid.UUID) error {
	if m.deleteDatabaseFn != nil {
		return m.deleteDatabaseFn(ctx, id, wsID)
	}
	return nil
}

// --- mock Authenticator ---

type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(_ context.Context, _ string) (*port.AuthResult, error) {
	return nil, nil
}

// --- helpers ---

func newTestServer(t *testing.T, repo *mockAdminRepo) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSvc := service.NewAdminService(repo, nil, nil, nil, logger)

	tunnelCfg := protocol.ServerTunnelConfig{
		Heartbeat: protocol.HeartbeatConfig{
			Interval:      10 * time.Second,
			Timeout:       5 * time.Second,
			MissThreshold: 3,
		},
		HandshakeTimeout: 10 * time.Second,
		Yamux: protocol.YamuxConfig{
			KeepAliveInterval:      15 * time.Second,
			ConnectionWriteTimeout: 10 * time.Second,
		},
	}
	registry := itunnel.NewTunnelRegistry(&mockAuthenticator{}, tunnelCfg, "0.1.0", logger)

	srv := New(Config{
		ListenAddr:        ":0",
		AdminSecret:       testAdminSecret,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MCPRateLimit:      60,
		AdminRateLimit:    30,
	}, registry, nil, &mockAuthenticator{}, adminSvc, nil, logger)

	return httptest.NewServer(srv.router)
}

func adminReq(t *testing.T, method, url string, body any) *http.Request {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+testAdminSecret)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// --- adminAuth middleware tests ---

func TestAdminAuth_ValidSecret(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/keys?workspace_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+testAdminSecret)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAdminAuth_WrongSecret(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/keys?workspace_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAdminAuth_MissingHeader(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/keys?workspace_id="+uuid.New().String(), nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// --- Create Key ---

func TestCreateKey_HappyPath(t *testing.T) {
	wsID := uuid.New()
	dbID := uuid.New()
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/keys", map[string]string{
		"name":         "my-key",
		"workspace_id": wsID.String(),
		"database_id":  dbID.String(),
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var body createKeyResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.NotEmpty(t, body.Key)
	assert.NotEmpty(t, body.ID)
	assert.Equal(t, "my-key", body.Name)
	assert.Equal(t, wsID.String(), body.WorkspaceID)
	assert.Equal(t, dbID.String(), body.DatabaseID)
}

func TestCreateKey_InvalidWorkspaceID(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/keys", map[string]string{
		"name":         "key",
		"workspace_id": "not-a-uuid",
		"database_id":  uuid.New().String(),
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateKey_MissingDatabaseID(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/keys", map[string]string{
		"name":         "key",
		"workspace_id": uuid.New().String(),
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateKey_RepoError(t *testing.T) {
	repo := &mockAdminRepo{
		createAPIKeyFn: func(_ context.Context, _, _ uuid.UUID, _, _, _ string) (*port.APIKeyRecord, error) {
			return nil, fmt.Errorf("db connection lost")
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/keys", map[string]string{
		"name":         "key",
		"workspace_id": uuid.New().String(),
		"database_id":  uuid.New().String(),
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Verify error message does NOT leak internals.
	body, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(body), "db connection lost")
}

// --- List Keys ---

func TestListKeys_HappyPath(t *testing.T) {
	wsID := uuid.New()
	dbID := uuid.New()
	now := time.Now().Truncate(time.Second)
	repo := &mockAdminRepo{
		listAPIKeysFn: func(_ context.Context, id uuid.UUID) ([]port.APIKeyRecord, error) {
			assert.Equal(t, wsID, id)
			return []port.APIKeyRecord{
				{ID: uuid.New(), KeyPrefix: "ism_abc...xyz", Name: "k1", DatabaseID: dbID, CreatedAt: now},
			}, nil
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "GET", ts.URL+"/api/keys?workspace_id="+wsID.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var keys []keyResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&keys))
	require.Len(t, keys, 1)
	assert.Equal(t, "k1", keys[0].Name)
	assert.Equal(t, dbID.String(), keys[0].DatabaseID)
}

func TestListKeys_MissingWorkspaceID(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "GET", ts.URL+"/api/keys", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- Delete Key ---

func TestDeleteKey_HappyPath(t *testing.T) {
	keyID := uuid.New()
	wsID := uuid.New()
	deleted := false
	repo := &mockAdminRepo{
		deleteAPIKeyFn: func(_ context.Context, id, ws uuid.UUID) error {
			assert.Equal(t, keyID, id)
			assert.Equal(t, wsID, ws)
			deleted = true
			return nil
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "DELETE", ts.URL+"/api/keys/"+keyID.String()+"?workspace_id="+wsID.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.True(t, deleted)
}

func TestDeleteKey_InvalidKeyID(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "DELETE", ts.URL+"/api/keys/not-a-uuid?workspace_id="+uuid.New().String(), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- Create Database ---

func TestCreateDatabase_Tunnel(t *testing.T) {
	wsID := uuid.New()
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/databases", map[string]string{
		"workspace_id":    wsID.String(),
		"name":            "my-db",
		"connection_type": "tunnel",
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var body databaseResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "my-db", body.Name)
	assert.Equal(t, "tunnel", body.ConnectionType)
	assert.Equal(t, "pending", body.Status)
}

func TestCreateDatabase_MissingName(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/databases", map[string]string{
		"workspace_id": uuid.New().String(),
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateDatabase_ErrorDoesNotLeakDetails(t *testing.T) {
	repo := &mockAdminRepo{
		createDatabaseFn: func(_ context.Context, _ uuid.UUID, _, _, _ string, _ []byte) (*port.DatabaseInfo, error) {
			return nil, fmt.Errorf("ENCRYPTION_KEY not set, internal path /etc/secrets")
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "POST", ts.URL+"/api/databases", map[string]string{
		"workspace_id":    uuid.New().String(),
		"name":            "db",
		"connection_type": "direct",
		"connection_url":  "postgres://localhost/test",
	})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(body), "ENCRYPTION_KEY")
	assert.NotContains(t, string(body), "/etc/secrets")
	assert.Contains(t, string(body), "failed to create database")
}

// --- List Databases ---

func TestListDatabases_HappyPath(t *testing.T) {
	wsID := uuid.New()
	repo := &mockAdminRepo{
		listDatabasesFn: func(_ context.Context, id uuid.UUID) ([]port.DatabaseInfo, error) {
			return []port.DatabaseInfo{
				{ID: uuid.New(), WorkspaceID: id, Name: "db1", ConnectionType: "tunnel", Status: "pending", CreatedAt: time.Now()},
			}, nil
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "GET", ts.URL+"/api/databases?workspace_id="+wsID.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var dbs []databaseResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&dbs))
	require.Len(t, dbs, 1)
	assert.Equal(t, "db1", dbs[0].Name)
}

// --- Delete Database ---

func TestDeleteDatabase_HappyPath(t *testing.T) {
	dbID := uuid.New()
	wsID := uuid.New()
	deleted := false
	repo := &mockAdminRepo{
		deleteDatabaseFn: func(_ context.Context, id, ws uuid.UUID) error {
			assert.Equal(t, dbID, id)
			assert.Equal(t, wsID, ws)
			deleted = true
			return nil
		},
	}
	ts := newTestServer(t, repo)
	defer ts.Close()

	req := adminReq(t, "DELETE", ts.URL+"/api/databases/"+dbID.String()+"?workspace_id="+wsID.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.True(t, deleted)
}

// --- Health ---

func TestHealth(t *testing.T) {
	ts := newTestServer(t, &mockAdminRepo{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
