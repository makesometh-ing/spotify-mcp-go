package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// generateRandomHex returns a hex-encoded string of n random bytes.
func generateRandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateToken returns a cryptographically random opaque token (32 bytes, hex-encoded).
func GenerateToken() (string, error) {
	return generateRandomHex(32)
}

// GenerateAuthCode returns a cryptographically random authorization code (32 bytes, hex-encoded).
func GenerateAuthCode() (string, error) {
	return generateRandomHex(32)
}

type issuedToken struct {
	clientID string
	expiry   time.Time
}

// TokenManager tracks issued MCP access tokens and validates them.
type TokenManager struct {
	mu     sync.RWMutex
	tokens map[string]issuedToken
	ttl    time.Duration
}

// NewTokenManager returns a TokenManager that issues tokens with the given TTL.
func NewTokenManager(ttl time.Duration) *TokenManager {
	return &TokenManager{
		tokens: make(map[string]issuedToken),
		ttl:    ttl,
	}
}

// IssueAccessToken generates a new access token for the given client, stores it,
// and returns the token string.
func (m *TokenManager) IssueAccessToken(clientID string) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[token] = issuedToken{
		clientID: clientID,
		expiry:   time.Now().Add(m.ttl),
	}
	return token, nil
}

// ValidateAccessToken checks if the token was issued and has not expired.
// Returns the associated client ID and true if valid.
func (m *TokenManager) ValidateAccessToken(token string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	issued, ok := m.tokens[token]
	if !ok {
		return "", false
	}
	if time.Now().After(issued.expiry) {
		return "", false
	}
	return issued.clientID, true
}
