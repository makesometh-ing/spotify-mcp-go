package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
