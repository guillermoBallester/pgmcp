package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock AdminRepository ---

type mockAdminRepo struct {
	createAPIKeyCalled bool
	lastCreateWSID     uuid.UUID
	lastCreateDBID     uuid.UUID
	lastCreateName     string
	createAPIKeyErr    error

	listAPIKeysResult []port.APIKeyRecord
	listAPIKeysErr    error

	deleteAPIKeyCalled bool
	deleteAPIKeyErr    error

	createDatabaseCalled bool
	lastCreateDBConnType string
	lastCreateDBStatus   string
	lastCreateDBEncURL   []byte
	createDatabaseResult *port.DatabaseInfo
	createDatabaseErr    error

	deleteDatabaseCalled bool
	deleteDatabaseErr    error
}

func (m *mockAdminRepo) CreateAPIKey(_ context.Context, wsID, dbID uuid.UUID, _, keyPrefix, name string) (*port.APIKeyRecord, error) {
	m.createAPIKeyCalled = true
	m.lastCreateWSID = wsID
	m.lastCreateDBID = dbID
	m.lastCreateName = name
	if m.createAPIKeyErr != nil {
		return nil, m.createAPIKeyErr
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

func (m *mockAdminRepo) ListAPIKeys(_ context.Context, _ uuid.UUID) ([]port.APIKeyRecord, error) {
	return m.listAPIKeysResult, m.listAPIKeysErr
}

func (m *mockAdminRepo) DeleteAPIKey(_ context.Context, _, _ uuid.UUID) error {
	m.deleteAPIKeyCalled = true
	return m.deleteAPIKeyErr
}

func (m *mockAdminRepo) CreateDatabase(_ context.Context, wsID uuid.UUID, name, connType, status string, encURL []byte) (*port.DatabaseInfo, error) {
	m.createDatabaseCalled = true
	m.lastCreateDBConnType = connType
	m.lastCreateDBStatus = status
	m.lastCreateDBEncURL = encURL
	if m.createDatabaseErr != nil {
		return nil, m.createDatabaseErr
	}
	if m.createDatabaseResult != nil {
		return m.createDatabaseResult, nil
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

func (m *mockAdminRepo) ListDatabases(_ context.Context, _ uuid.UUID) ([]port.DatabaseInfo, error) {
	return nil, nil
}

func (m *mockAdminRepo) DeleteDatabase(_ context.Context, _, _ uuid.UUID) error {
	m.deleteDatabaseCalled = true
	return m.deleteDatabaseErr
}

// --- mock Encryptor ---

type mockEncryptor struct {
	encryptResult []byte
	encryptErr    error
	decryptResult []byte
	decryptErr    error
}

func (m *mockEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if m.encryptErr != nil {
		return nil, m.encryptErr
	}
	if m.encryptResult != nil {
		return m.encryptResult, nil
	}
	// Simple reversible "encryption" for tests.
	return append([]byte("enc:"), plaintext...), nil
}

func (m *mockEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if m.decryptErr != nil {
		return nil, m.decryptErr
	}
	return m.decryptResult, nil
}

// --- helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- CreateAPIKey tests ---

func TestCreateAPIKey_HappyPath(t *testing.T) {
	repo := &mockAdminRepo{}
	svc := NewAdminService(repo, nil, nil, testLogger())

	wsID := uuid.New()
	dbID := uuid.New()
	fullKey, record, err := svc.CreateAPIKey(context.Background(), wsID, dbID, "test-key")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.NotEmpty(t, fullKey)
	assert.True(t, repo.createAPIKeyCalled)
	assert.Equal(t, wsID, repo.lastCreateWSID)
	assert.Equal(t, dbID, repo.lastCreateDBID)
	assert.Equal(t, "test-key", repo.lastCreateName)
}

func TestCreateAPIKey_RepoError(t *testing.T) {
	repo := &mockAdminRepo{createAPIKeyErr: fmt.Errorf("db error")}
	svc := NewAdminService(repo, nil, nil, testLogger())

	_, _, err := svc.CreateAPIKey(context.Background(), uuid.New(), uuid.New(), "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing api key")
}

// --- CreateDatabase tests ---

func TestCreateDatabase_Tunnel(t *testing.T) {
	repo := &mockAdminRepo{}
	svc := NewAdminService(repo, nil, nil, testLogger())

	info, err := svc.CreateDatabase(context.Background(), uuid.New(), "my-db", "tunnel", "")
	require.NoError(t, err)
	assert.Equal(t, "tunnel", info.ConnectionType)
	assert.Equal(t, "pending", info.Status)
	assert.Equal(t, "tunnel", repo.lastCreateDBConnType)
	assert.Equal(t, "pending", repo.lastCreateDBStatus)
	assert.Nil(t, repo.lastCreateDBEncURL)
}

func TestCreateDatabase_DefaultsToTunnel(t *testing.T) {
	repo := &mockAdminRepo{}
	svc := NewAdminService(repo, nil, nil, testLogger())

	_, err := svc.CreateDatabase(context.Background(), uuid.New(), "my-db", "", "")
	require.NoError(t, err)
	assert.Equal(t, "tunnel", repo.lastCreateDBConnType)
}

func TestCreateDatabase_Direct(t *testing.T) {
	repo := &mockAdminRepo{}
	enc := &mockEncryptor{}
	svc := NewAdminService(repo, enc, nil, testLogger())

	info, err := svc.CreateDatabase(context.Background(), uuid.New(), "my-db", "direct", "postgres://localhost/test")
	require.NoError(t, err)
	assert.Equal(t, "direct", info.ConnectionType)
	assert.Equal(t, "ready", info.Status)
	assert.Equal(t, []byte("enc:postgres://localhost/test"), repo.lastCreateDBEncURL)
}

func TestCreateDatabase_Direct_MissingURL(t *testing.T) {
	svc := NewAdminService(&mockAdminRepo{}, &mockEncryptor{}, nil, testLogger())

	_, err := svc.CreateDatabase(context.Background(), uuid.New(), "db", "direct", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection_url is required")
}

func TestCreateDatabase_Direct_NoEncryptor(t *testing.T) {
	svc := NewAdminService(&mockAdminRepo{}, nil, nil, testLogger())

	_, err := svc.CreateDatabase(context.Background(), uuid.New(), "db", "direct", "postgres://x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestCreateDatabase_Direct_EncryptError(t *testing.T) {
	enc := &mockEncryptor{encryptErr: fmt.Errorf("bad key")}
	svc := NewAdminService(&mockAdminRepo{}, enc, nil, testLogger())

	_, err := svc.CreateDatabase(context.Background(), uuid.New(), "db", "direct", "postgres://x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypting connection URL")
}

// --- DeleteDatabase tests ---

func TestDeleteDatabase_HappyPath(t *testing.T) {
	repo := &mockAdminRepo{}
	svc := NewAdminService(repo, nil, nil, testLogger())

	err := svc.DeleteDatabase(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.True(t, repo.deleteDatabaseCalled)
}

func TestDeleteDatabase_RepoError(t *testing.T) {
	repo := &mockAdminRepo{deleteDatabaseErr: fmt.Errorf("not found")}
	svc := NewAdminService(repo, nil, nil, testLogger())

	err := svc.DeleteDatabase(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}

// --- ListAPIKeys ---

func TestListAPIKeys_Delegates(t *testing.T) {
	wsID := uuid.New()
	expected := []port.APIKeyRecord{{ID: uuid.New(), Name: "k1"}}
	repo := &mockAdminRepo{listAPIKeysResult: expected}
	svc := NewAdminService(repo, nil, nil, testLogger())

	result, err := svc.ListAPIKeys(context.Background(), wsID)
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

// --- DeleteAPIKey ---

func TestDeleteAPIKey_Delegates(t *testing.T) {
	repo := &mockAdminRepo{}
	svc := NewAdminService(repo, nil, nil, testLogger())

	err := svc.DeleteAPIKey(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.True(t, repo.deleteAPIKeyCalled)
}
