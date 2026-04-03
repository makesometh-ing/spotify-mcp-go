// Package tools provides MCP tool registration and dispatch.
package tools

import (
	"context"
	"fmt"
	"net/http"

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
// up via the auth context's client ID) into every outbound request.
func Register(srv *mcpserver.MCPServer, registrations []ToolRegistration, tokenStore store.TokenStore, baseURL string) {
	client := &http.Client{
		Transport: &spotifyTransport{
			store: tokenStore,
			base:  http.DefaultTransport,
		},
	}
	for _, reg := range registrations {
		srv.AddTool(reg.Tool, reg.NewHandler(baseURL, client))
	}
}

// spotifyTransport is an http.RoundTripper that injects the Spotify access token
// from the request context into outbound requests as a Bearer header.
type spotifyTransport struct {
	store store.TokenStore
	base  http.RoundTripper
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

	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+record.SpotifyAccessToken)
	return t.base.RoundTrip(r2)
}
