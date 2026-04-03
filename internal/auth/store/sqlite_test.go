package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSQLiteStore(t *testing.T) *SQLiteTokenStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tokens.db")
	s, err := NewSQLiteTokenStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteRoundTrip(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	record := &TokenRecord{
		SpotifyAccessToken:  "access-123",
		SpotifyRefreshToken: "refresh-456",
		SpotifyTokenExpiry:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		MCPAccessToken:      "mcp-access",
		MCPRefreshToken:     "mcp-refresh",
		MCPTokenExpiry:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	err := s.Store(ctx, "client-1", record)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "client-1")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, record.SpotifyAccessToken, loaded.SpotifyAccessToken)
	assert.Equal(t, record.SpotifyRefreshToken, loaded.SpotifyRefreshToken)
	assert.True(t, record.SpotifyTokenExpiry.Equal(loaded.SpotifyTokenExpiry))
	assert.Equal(t, record.MCPAccessToken, loaded.MCPAccessToken)
	assert.Equal(t, record.MCPRefreshToken, loaded.MCPRefreshToken)
	assert.True(t, record.MCPTokenExpiry.Equal(loaded.MCPTokenExpiry))
	assert.True(t, record.CreatedAt.Equal(loaded.CreatedAt))
}

func TestSQLitePersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tokens.db")
	ctx := context.Background()

	// Write with first instance
	s1, err := NewSQLiteTokenStore(dbPath)
	require.NoError(t, err)
	err = s1.Store(ctx, "persist-client", &TokenRecord{
		SpotifyAccessToken: "persisted-token",
		CreatedAt:          time.Now(),
	})
	require.NoError(t, err)
	s1.Close()

	// Read with second instance
	s2, err := NewSQLiteTokenStore(dbPath)
	require.NoError(t, err)
	defer s2.Close()

	loaded, err := s2.Load(ctx, "persist-client")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "persisted-token", loaded.SpotifyAccessToken)
}

func TestSQLiteLoadNonExistent(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	loaded, err := s.Load(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSQLiteDelete(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	err := s.Store(ctx, "del-client", &TokenRecord{
		SpotifyAccessToken: "to-delete",
		CreatedAt:          time.Now(),
	})
	require.NoError(t, err)

	err = s.Delete(ctx, "del-client")
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "del-client")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSQLiteTTLCleanupRemovesExpired(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// Store a record with old CreatedAt
	err := s.Store(ctx, "old-client", &TokenRecord{
		SpotifyAccessToken: "old-token",
		CreatedAt:          time.Now().Add(-48 * time.Hour),
	})
	require.NoError(t, err)

	// Cleanup with 24h TTL should remove it
	removed, err := s.CleanupExpired(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	loaded, err := s.Load(ctx, "old-client")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSQLiteTTLCleanupKeepsFresh(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// Store a recent record
	err := s.Store(ctx, "fresh-client", &TokenRecord{
		SpotifyAccessToken: "fresh-token",
		CreatedAt:          time.Now(),
	})
	require.NoError(t, err)

	// Cleanup with 24h TTL should NOT remove it
	removed, err := s.CleanupExpired(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed)

	loaded, err := s.Load(ctx, "fresh-client")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "fresh-token", loaded.SpotifyAccessToken)
}

func TestSQLiteConcurrent(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			clientID := fmt.Sprintf("concurrent-%d", n)
			err := s.Store(ctx, clientID, &TokenRecord{
				SpotifyAccessToken: fmt.Sprintf("token-%d", n),
				CreatedAt:          time.Now(),
			})
			assert.NoError(t, err)

			loaded, err := s.Load(ctx, clientID)
			assert.NoError(t, err)
			assert.NotNil(t, loaded)
		}(i)
	}
	wg.Wait()
}

func TestSQLiteUpsert(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	err := s.Store(ctx, "upsert-client", &TokenRecord{
		SpotifyAccessToken: "first",
		CreatedAt:          time.Now(),
	})
	require.NoError(t, err)

	err = s.Store(ctx, "upsert-client", &TokenRecord{
		SpotifyAccessToken: "second",
		CreatedAt:          time.Now(),
	})
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "upsert-client")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "second", loaded.SpotifyAccessToken)
}
