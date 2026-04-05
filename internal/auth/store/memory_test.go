package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRecord(suffix string) *TokenRecord {
	now := time.Now()
	return &TokenRecord{
		SpotifyAccessToken:  "sp_access_" + suffix,
		SpotifyRefreshToken: "sp_refresh_" + suffix,
		SpotifyTokenExpiry:  now.Add(time.Hour),
		MCPAccessToken:      "mcp_access_" + suffix,
		MCPRefreshToken:     "mcp_refresh_" + suffix,
		MCPTokenExpiry:      now.Add(2 * time.Hour),
		CreatedAt:           now,
	}
}

func TestInMemoryStoreLoadRoundTrip(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()
	record := newTestRecord("1")

	err := s.Store(ctx, "client-1", record)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Equal(t, record, loaded)
}

func TestInMemoryLoadNonExistent(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	loaded, err := s.Load(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestInMemoryDeleteRemovesRecord(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	err := s.Store(ctx, "client-1", newTestRecord("1"))
	require.NoError(t, err)

	err = s.Delete(ctx, "client-1")
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestInMemoryDeleteNonExistent(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	err := s.Delete(ctx, "does-not-exist")
	require.NoError(t, err)
}

func TestInMemoryConcurrentWrites(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			clientID := fmt.Sprintf("client-%d", id)
			record := newTestRecord(fmt.Sprintf("%d", id))
			err := s.Store(ctx, clientID, record)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		clientID := fmt.Sprintf("client-%d", i)
		loaded, err := s.Load(ctx, clientID)
		require.NoError(t, err)
		require.NotNil(t, loaded)
		assert.Equal(t, "sp_access_"+fmt.Sprintf("%d", i), loaded.SpotifyAccessToken)
	}
}

func TestInMemoryLoadAll(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	err := s.Store(ctx, "client-a", newTestRecord("a"))
	require.NoError(t, err)
	err = s.Store(ctx, "client-b", newTestRecord("b"))
	require.NoError(t, err)

	records, err := s.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "sp_access_a", records["client-a"].SpotifyAccessToken)
	assert.Equal(t, "sp_access_b", records["client-b"].SpotifyAccessToken)
}

func TestInMemoryLoadAllEmpty(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	records, err := s.LoadAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestInMemoryStoreOverwrites(t *testing.T) {
	s := NewInMemoryTokenStore()
	ctx := context.Background()

	first := newTestRecord("first")
	second := newTestRecord("second")

	err := s.Store(ctx, "client-1", first)
	require.NoError(t, err)

	err = s.Store(ctx, "client-1", second)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "client-1")
	require.NoError(t, err)
	assert.Equal(t, second, loaded)
}
