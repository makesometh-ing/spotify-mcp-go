package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

func TestMCPTokenEntropy(t *testing.T) {
	token, err := GenerateToken()
	require.NoError(t, err)
	// 32 bytes hex-encoded = 64 chars
	assert.GreaterOrEqual(t, len(token), 64)
}

func TestMCPTokenRandomness(t *testing.T) {
	t1, err := GenerateToken()
	require.NoError(t, err)
	t2, err := GenerateToken()
	require.NoError(t, err)
	assert.NotEqual(t, t1, t2)
}

func TestMCPTokenManagerValidateWithinExpiry(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	token, err := mgr.IssueAccessToken("client-1")
	require.NoError(t, err)

	clientID, ok := mgr.ValidateAccessToken(token)
	assert.True(t, ok)
	assert.Equal(t, "client-1", clientID)
}

func TestMCPTokenManagerValidateExpired(t *testing.T) {
	mgr := NewTokenManager(-time.Second) // already expired

	token, err := mgr.IssueAccessToken("client-1")
	require.NoError(t, err)

	_, ok := mgr.ValidateAccessToken(token)
	assert.False(t, ok)
}

func TestMCPTokenManagerValidateUnknownToken(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	_, ok := mgr.ValidateAccessToken("not-a-real-token")
	assert.False(t, ok)
}

func TestMCPTokenAuthCodeEntropy(t *testing.T) {
	code, err := GenerateAuthCode()
	require.NoError(t, err)
	// 32 bytes hex-encoded = 64 chars
	assert.GreaterOrEqual(t, len(code), 64)
}

func TestMCPTokenAuthCodeRandomness(t *testing.T) {
	c1, err := GenerateAuthCode()
	require.NoError(t, err)
	c2, err := GenerateAuthCode()
	require.NoError(t, err)
	assert.NotEqual(t, c1, c2)
}

func TestTokenManagerHydrateAccessToken(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	records := map[string]*store.TokenRecord{
		"client-1": {
			MCPAccessToken: "hydrated-access",
			MCPTokenExpiry: time.Now().Add(30 * time.Minute),
		},
	}
	mgr.Hydrate(records)

	clientID, ok := mgr.ValidateAccessToken("hydrated-access")
	assert.True(t, ok)
	assert.Equal(t, "client-1", clientID)
}

func TestTokenManagerHydrateExpiredAccessToken(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	records := map[string]*store.TokenRecord{
		"client-1": {
			MCPAccessToken: "expired-access",
			MCPTokenExpiry: time.Now().Add(-time.Hour),
		},
	}
	mgr.Hydrate(records)

	_, ok := mgr.ValidateAccessToken("expired-access")
	assert.False(t, ok, "expired tokens should not be valid after hydration")
}

func TestTokenManagerHydrateRefreshToken(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	records := map[string]*store.TokenRecord{
		"client-1": {
			MCPRefreshToken: "hydrated-refresh",
		},
	}
	mgr.Hydrate(records)

	clientID, ok := mgr.ValidateRefreshToken("hydrated-refresh")
	assert.True(t, ok)
	assert.Equal(t, "client-1", clientID)
}

func TestTokenManagerHydrateSkipsEmptyTokens(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	records := map[string]*store.TokenRecord{
		"client-1": {
			MCPAccessToken:  "",
			MCPRefreshToken: "",
		},
	}
	mgr.Hydrate(records)

	_, ok := mgr.ValidateAccessToken("")
	assert.False(t, ok)
	_, ok = mgr.ValidateRefreshToken("")
	assert.False(t, ok)
}

func TestTokenManagerHydrateMultipleClients(t *testing.T) {
	mgr := NewTokenManager(time.Hour)

	records := map[string]*store.TokenRecord{
		"client-a": {
			MCPAccessToken:  "access-a",
			MCPRefreshToken: "refresh-a",
			MCPTokenExpiry:  time.Now().Add(30 * time.Minute),
		},
		"client-b": {
			MCPAccessToken:  "access-b",
			MCPRefreshToken: "refresh-b",
			MCPTokenExpiry:  time.Now().Add(30 * time.Minute),
		},
	}
	mgr.Hydrate(records)

	clientID, ok := mgr.ValidateAccessToken("access-a")
	assert.True(t, ok)
	assert.Equal(t, "client-a", clientID)

	clientID, ok = mgr.ValidateAccessToken("access-b")
	assert.True(t, ok)
	assert.Equal(t, "client-b", clientID)

	clientID, ok = mgr.ValidateRefreshToken("refresh-a")
	assert.True(t, ok)
	assert.Equal(t, "client-a", clientID)
}
