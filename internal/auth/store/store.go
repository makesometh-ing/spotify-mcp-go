// Package store defines the TokenStore interface and its implementations.
package store

import (
	"context"
	"time"
)

// TokenRecord holds the Spotify and MCP tokens for a registered client.
type TokenRecord struct {
	SpotifyAccessToken  string
	SpotifyRefreshToken string
	SpotifyTokenExpiry  time.Time
	MCPAccessToken      string
	MCPRefreshToken     string
	MCPTokenExpiry      time.Time
	CreatedAt           time.Time

	// Registration metadata (RFC 7591)
	RedirectURIs            []string
	GrantTypes              []string
	ResponseTypes           []string
	TokenEndpointAuthMethod string
	ClientName              string
}

// TokenStore persists token records keyed by MCP client ID.
type TokenStore interface {
	Store(ctx context.Context, clientID string, tokens *TokenRecord) error
	Load(ctx context.Context, clientID string) (*TokenRecord, error)
	Delete(ctx context.Context, clientID string) error
	LoadAll(ctx context.Context) (map[string]*TokenRecord, error)
}
