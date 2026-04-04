// Package tools provides MCP tool registration and dispatch.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

// HandlerFactory creates a tool handler bound to the typed Spotify client.
type HandlerFactory func(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// ToolRegistration pairs a tool definition with its handler factory.
type ToolRegistration struct {
	Tool       mcp.Tool
	NewHandler HandlerFactory
}

// Register registers all tool registrations with the MCP server. It creates a
// Spotify ClientWithResponses whose HTTP transport injects the caller's Spotify
// access token and handles transparent refresh.
func Register(srv *mcpserver.MCPServer, registrations []ToolRegistration, tokenStore store.TokenStore, spotifyClient *auth.SpotifyClient, baseURL string, logger *zap.SugaredLogger) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	logger = logger.Named("tools")

	var refresher *auth.TokenRefresher
	if spotifyClient != nil {
		refresher = auth.NewTokenRefresher(tokenStore, spotifyClient)
	}

	httpClient := &http.Client{
		Transport: &spotifyTransport{
			store:     tokenStore,
			refresher: refresher,
			base:      http.DefaultTransport,
			logger:    logger.Named("spotify"),
		},
	}

	client, _ := spotify.NewClientWithResponses(baseURL, spotify.WithHTTPClient(httpClient))

	for _, reg := range registrations {
		handler := reg.NewHandler(client)
		toolName := reg.Tool.Name
		srv.AddTool(reg.Tool, loggingHandler(logger, toolName, handler))
	}
}

// loggingHandler wraps a tool handler to log invocation details.
func loggingHandler(logger *zap.SugaredLogger, toolName string, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		result, err := handler(ctx, req)
		duration := time.Since(start)

		success := err == nil && (result == nil || !result.IsError)
		var response string
		if result != nil && len(result.Content) > 0 {
			if tc, ok := result.Content[0].(mcp.TextContent); ok {
				response = tc.Text
			}
		}

		logger.Infow("tool invocation",
			"tool", toolName,
			"args", req.Params.Arguments,
			"duration", duration,
			"success", success,
			"response", response,
		)
		return result, err
	}
}

// spotifyTransport is an http.RoundTripper that injects the Spotify access token
// from the request context into outbound requests as a Bearer header. If a
// TokenRefresher is configured, expired tokens are refreshed transparently.
type spotifyTransport struct {
	store     store.TokenStore
	refresher *auth.TokenRefresher
	base      http.RoundTripper
	logger    *zap.SugaredLogger
}

func (t *spotifyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientID, ok := auth.ClientIDFromContext(r.Context())
	if !ok {
		return nil, fmt.Errorf("no authenticated client in context")
	}

	var accessToken string
	if t.refresher != nil {
		token, err := t.refresher.GetAccessToken(r.Context(), clientID)
		if err != nil {
			return nil, err
		}
		accessToken = token
	} else {
		record, err := t.store.Load(r.Context(), clientID)
		if err != nil {
			return nil, fmt.Errorf("loading tokens for client %s: %w", clientID, err)
		}
		if record == nil {
			return nil, fmt.Errorf("no tokens found for client %s", clientID)
		}
		accessToken = record.SpotifyAccessToken
	}

	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+accessToken)

	start := time.Now()
	resp, err := t.base.RoundTrip(r2)
	duration := time.Since(start)

	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	t.logger.Debugw("spotify api call",
		"endpoint", r.URL.Path,
		"status", status,
		"duration", duration,
		"error", err,
	)
	return resp, err
}
