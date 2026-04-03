package auth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

// TokenRefresher handles transparent Spotify token refresh. It checks whether
// the stored access token is expired and, if so, refreshes it using the Spotify
// refresh token. Concurrent refresh requests for the same client are coalesced
// via singleflight to prevent thundering herd.
type TokenRefresher struct {
	store         store.TokenStore
	spotifyClient *SpotifyClient
	group         singleflight.Group
}

// NewTokenRefresher creates a TokenRefresher.
func NewTokenRefresher(tokenStore store.TokenStore, spotifyClient *SpotifyClient) *TokenRefresher {
	return &TokenRefresher{
		store:         tokenStore,
		spotifyClient: spotifyClient,
	}
}

// GetAccessToken returns a valid Spotify access token for the given client ID.
// If the stored token is expired, it transparently refreshes it first.
func (r *TokenRefresher) GetAccessToken(ctx context.Context, clientID string) (string, error) {
	record, err := r.store.Load(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("loading tokens for %s: %w", clientID, err)
	}
	if record == nil {
		return "", fmt.Errorf("no tokens found for client %s", clientID)
	}

	// Token still valid
	if record.SpotifyTokenExpiry.IsZero() || time.Now().Before(record.SpotifyTokenExpiry) {
		return record.SpotifyAccessToken, nil
	}

	// Token expired, refresh (coalesced per client)
	token, err, _ := r.group.Do(clientID, func() (any, error) {
		return r.refresh(ctx, clientID, record)
	})
	if err != nil {
		return "", err
	}
	return token.(string), nil
}

func (r *TokenRefresher) refresh(ctx context.Context, clientID string, record *store.TokenRecord) (string, error) {
	tokenResp, err := r.spotifyClient.RefreshToken(ctx, record.SpotifyRefreshToken)
	if err != nil {
		return "", err
	}

	// Create a new record rather than mutating the shared pointer from the store.
	refreshToken := record.SpotifyRefreshToken
	if tokenResp.RefreshToken != "" {
		refreshToken = tokenResp.RefreshToken
	}
	updated := &store.TokenRecord{
		SpotifyAccessToken:  tokenResp.AccessToken,
		SpotifyRefreshToken: refreshToken,
		SpotifyTokenExpiry:  time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		MCPAccessToken:      record.MCPAccessToken,
		MCPRefreshToken:     record.MCPRefreshToken,
		MCPTokenExpiry:      record.MCPTokenExpiry,
		CreatedAt:           record.CreatedAt,
	}

	if err := r.store.Store(ctx, clientID, updated); err != nil {
		return "", fmt.Errorf("storing refreshed tokens: %w", err)
	}

	return tokenResp.AccessToken, nil
}
