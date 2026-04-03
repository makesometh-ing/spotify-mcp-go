package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// verifierCharset is the set of unreserved URL-safe characters per RFC 7636.
const verifierCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._~-"

// GenerateCodeVerifier returns a cryptographically random code verifier
// of 64 characters using only unreserved URL-safe characters.
func GenerateCodeVerifier() (string, error) {
	const length = 64
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = verifierCharset[int(buf[i])%len(verifierCharset)]
	}
	return string(buf), nil
}

// CodeChallenge computes the S256 code challenge for the given verifier:
// BASE64URL(SHA256(verifier)).
func CodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// VerifyCodeChallenge returns true if the challenge matches the S256 hash
// of the verifier.
func VerifyCodeChallenge(verifier, challenge string) bool {
	return CodeChallenge(verifier) == challenge
}
