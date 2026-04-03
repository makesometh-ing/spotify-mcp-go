package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPKCEVerifierLength(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(verifier), 43)
	assert.LessOrEqual(t, len(verifier), 128)
}

func TestPKCEVerifierCharset(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	matched, err := regexp.MatchString(`^[A-Za-z0-9._~-]+$`, verifier)
	require.NoError(t, err)
	assert.True(t, matched, "verifier contains invalid characters: %s", verifier)
}

func TestPKCEVerifierRandomness(t *testing.T) {
	v1, err := GenerateCodeVerifier()
	require.NoError(t, err)
	v2, err := GenerateCodeVerifier()
	require.NoError(t, err)
	assert.NotEqual(t, v1, v2)
}

func TestPKCES256Challenge(t *testing.T) {
	// Use a known verifier and manually compute the expected challenge.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	hash := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(hash[:])

	challenge := CodeChallenge(verifier)
	assert.Equal(t, expected, challenge)
}

func TestPKCEVerifyMatching(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	challenge := CodeChallenge(verifier)
	assert.True(t, VerifyCodeChallenge(verifier, challenge))
}

func TestPKCEVerifyTamperedVerifier(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	challenge := CodeChallenge(verifier)
	assert.False(t, VerifyCodeChallenge(verifier+"tampered", challenge))
}

func TestPKCEVerifyTamperedChallenge(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	challenge := CodeChallenge(verifier)
	assert.False(t, VerifyCodeChallenge(verifier, challenge+"tampered"))
}
