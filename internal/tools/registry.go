// Package tools provides MCP tool registration and dispatch.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

// HandlerFactory creates a tool handler bound to the given base URL and HTTP client.
type HandlerFactory func(baseURL string, httpClient *http.Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// ToolRegistration pairs a tool definition with its handler factory.
type ToolRegistration struct {
	Tool       mcp.Tool
	NewHandler HandlerFactory
}

// Register registers all tool registrations with the MCP server. It creates an
// HTTP client whose transport injects the caller's Spotify access token (looked
// up via the auth context's client ID) into every outbound request, refreshing
// expired tokens transparently.
func Register(srv *mcpserver.MCPServer, registrations []ToolRegistration, tokenStore store.TokenStore, spotifyClient *auth.SpotifyClient, baseURL string) {
	client := &http.Client{
		Transport: &spotifyTransport{
			store:         tokenStore,
			spotifyClient: spotifyClient,
			base:          http.DefaultTransport,
		},
	}
	for _, reg := range registrations {
		srv.AddTool(reg.Tool, reg.NewHandler(baseURL, client))
	}
}

// spotifyTransport is an http.RoundTripper that injects the Spotify access token
// from the request context into outbound requests as a Bearer header. If the
// token is expired, it refreshes transparently before the request.
type spotifyTransport struct {
	store         store.TokenStore
	spotifyClient *auth.SpotifyClient
	base          http.RoundTripper
}

func (t *spotifyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientID, ok := auth.ClientIDFromContext(r.Context())
	if !ok {
		return nil, fmt.Errorf("no authenticated client in context")
	}

	record, err := t.store.Load(r.Context(), clientID)
	if err != nil {
		return nil, fmt.Errorf("loading tokens for client %s: %w", clientID, err)
	}
	if record == nil {
		return nil, fmt.Errorf("no tokens found for client %s", clientID)
	}

	// Transparent refresh: if Spotify token is expired and we have a refresh token,
	// refresh before making the API call.
	if t.spotifyClient != nil && record.SpotifyRefreshToken != "" &&
		!record.SpotifyTokenExpiry.IsZero() && time.Now().After(record.SpotifyTokenExpiry) {
		tokenResp, err := t.spotifyClient.RefreshToken(r.Context(), record.SpotifyRefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refreshing Spotify token: %w", err)
		}
		record.SpotifyAccessToken = tokenResp.AccessToken
		if tokenResp.RefreshToken != "" {
			record.SpotifyRefreshToken = tokenResp.RefreshToken
		}
		record.SpotifyTokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		if err := t.store.Store(r.Context(), clientID, record); err != nil {
			return nil, fmt.Errorf("storing refreshed tokens: %w", err)
		}
	}

	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+record.SpotifyAccessToken)
	return t.base.RoundTrip(r2)
}
