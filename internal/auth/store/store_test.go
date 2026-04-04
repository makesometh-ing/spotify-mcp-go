package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTokenRecordFields(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)

	record := &TokenRecord{
		SpotifyAccessToken:  "sp_access",
		SpotifyRefreshToken: "sp_refresh",
		SpotifyTokenExpiry:  expiry,
		MCPAccessToken:      "mcp_access",
		MCPRefreshToken:     "mcp_refresh",
		MCPTokenExpiry:      expiry.Add(2 * time.Hour),
		CreatedAt:           now,
	}

	assert.Equal(t, "sp_access", record.SpotifyAccessToken)
	assert.Equal(t, "sp_refresh", record.SpotifyRefreshToken)
	assert.Equal(t, expiry, record.SpotifyTokenExpiry)
	assert.Equal(t, "mcp_access", record.MCPAccessToken)
	assert.Equal(t, "mcp_refresh", record.MCPRefreshToken)
	assert.Equal(t, expiry.Add(2*time.Hour), record.MCPTokenExpiry)
	assert.Equal(t, now, record.CreatedAt)
}

func TestTokenStoreInterfaceCompliance(t *testing.T) {
	// Verify that the interface has the expected method signatures
	// by assigning a mock implementation to the interface type.
	var _ TokenStore = &mockTokenStore{}
}

// mockTokenStore is a minimal implementation to verify the interface contract.
type mockTokenStore struct {
	records map[string]*TokenRecord
}

func (m *mockTokenStore) Store(ctx context.Context, clientID string, tokens *TokenRecord) error {
	m.records[clientID] = tokens
	return nil
}

func (m *mockTokenStore) Load(ctx context.Context, clientID string) (*TokenRecord, error) {
	r, ok := m.records[clientID]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockTokenStore) Delete(ctx context.Context, clientID string) error {
	delete(m.records, clientID)
	return nil
}

func TestLoggingTokenStoreLogsOperations(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()

	inner := NewInMemoryTokenStore()
	store := NewLoggingTokenStore(inner, logger)
	ctx := context.Background()

	record := &TokenRecord{SpotifyAccessToken: "tok"}

	// Store
	err := store.Store(ctx, "client-1", record)
	require.NoError(t, err)

	// Load
	loaded, err := store.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Equal(t, "tok", loaded.SpotifyAccessToken)

	// Delete
	err = store.Delete(ctx, "client-1")
	require.NoError(t, err)

	// Verify log messages
	messages := make([]string, 0, logs.Len())
	for _, entry := range logs.All() {
		messages = append(messages, entry.Message)
	}
	assert.Contains(t, messages, "token store: store")
	assert.Contains(t, messages, "token store: load")
	assert.Contains(t, messages, "token store: delete")
}

func TestLoggingTokenStoreImplementsInterface(t *testing.T) {
	var _ TokenStore = &LoggingTokenStore{}
}

func TestMockTokenStoreRoundTrip(t *testing.T) {
	store := &mockTokenStore{records: make(map[string]*TokenRecord)}
	ctx := context.Background()

	now := time.Now()
	record := &TokenRecord{
		SpotifyAccessToken:  "access",
		SpotifyRefreshToken: "refresh",
		SpotifyTokenExpiry:  now.Add(time.Hour),
		MCPAccessToken:      "mcp_access",
		MCPRefreshToken:     "mcp_refresh",
		MCPTokenExpiry:      now.Add(2 * time.Hour),
		CreatedAt:           now,
	}

	// Store
	err := store.Store(ctx, "client-1", record)
	require.NoError(t, err)

	// Load
	loaded, err := store.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Equal(t, record, loaded)

	// Load non-existent
	missing, err := store.Load(ctx, "client-999")
	require.NoError(t, err)
	assert.Nil(t, missing)

	// Delete
	err = store.Delete(ctx, "client-1")
	require.NoError(t, err)

	deleted, err := store.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Nil(t, deleted)
}
