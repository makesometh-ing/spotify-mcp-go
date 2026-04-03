package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/makesometh-ing/spotify-mcp-go/internal/tools"
)

type serverConfig struct {
	Port                 string
	SpotifyClientID      string
	SpotifyClientSecret  string
	TokenDBPath          string
	SpotifyTokenEndpoint string // override for testing; empty = Spotify default
	SpotifyAPIBaseURL    string // override for testing; empty = https://api.spotify.com
}

// loadConfig reads configuration from environment variables and an optional .env file.
// Environment variables take precedence over .env file values.
func loadConfig(envFilePath string) (*serverConfig, error) {
	envMap := make(map[string]string)
	if envFilePath != "" {
		m, err := readEnvFile(envFilePath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading .env: %w", err)
		}
		if m != nil {
			envMap = m
		}
	}

	get := func(key, defaultValue string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v, ok := envMap[key]; ok && v != "" {
			return v
		}
		return defaultValue
	}

	cfg := &serverConfig{
		Port:                get("SPOTIFY_MCP_PORT", "8080"),
		SpotifyClientID:     get("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: get("SPOTIFY_CLIENT_SECRET", ""),
		TokenDBPath:         get("SPOTIFY_MCP_TOKEN_DB", "~/.config/spotify-mcp-go/auth/tokens.db"),
	}

	if cfg.SpotifyClientID == "" {
		return nil, fmt.Errorf("SPOTIFY_CLIENT_ID is required but not set")
	}
	if cfg.SpotifyClientSecret == "" {
		return nil, fmt.Errorf("SPOTIFY_CLIENT_SECRET is required but not set")
	}

	return cfg, nil
}

func printStartupInfo(out io.Writer, port string) {
	_, _ = fmt.Fprintf(out, "MCP endpoint: http://localhost:%s/mcp\n", port)
	_, _ = fmt.Fprintf(out, "Callback URL: http://localhost:%s/callback\n", port)
	_, _ = fmt.Fprintf(out, "SPOTIFY_CLIENT_ID: set\n")
	_, _ = fmt.Fprintf(out, "SPOTIFY_CLIENT_SECRET: set\n")
	_, _ = fmt.Fprintf(out, "\nConfigure the callback URL above as a Redirect URI in your Spotify Developer Dashboard at https://developer.spotify.com/dashboard\n")
}

// run starts the MCP server. It blocks until ctx is cancelled. When the server
// is ready to accept connections, the listen address is sent on addrCh (if non-nil).
// toolRegs may be nil if no tools are registered.
func run(ctx context.Context, cfg *serverConfig, toolRegs []tools.ToolRegistration, out io.Writer, addrCh chan<- string) error {
	tokenStore := store.NewInMemoryTokenStore()

	spotifyClient := &auth.SpotifyClient{
		ClientID:      cfg.SpotifyClientID,
		ClientSecret:  cfg.SpotifyClientSecret,
		TokenEndpoint: cfg.SpotifyTokenEndpoint,
	}

	authHandler := auth.NewHandler(auth.HandlerConfig{
		SpotifyClientID:      cfg.SpotifyClientID,
		SpotifyClientSecret:  cfg.SpotifyClientSecret,
		Store:                tokenStore,
		SpotifyTokenEndpoint: cfg.SpotifyTokenEndpoint,
	})

	mcpServer := server.NewMCPServer("spotify-mcp-go", "1.0.0",
		server.WithToolCapabilities(false),
	)

	apiBase := cfg.SpotifyAPIBaseURL
	if apiBase == "" {
		apiBase = "https://api.spotify.com"
	}
	if toolRegs != nil {
		tools.Register(mcpServer, toolRegs, tokenStore, spotifyClient, apiBase)
	}

	httpTransport := server.NewStreamableHTTPServer(mcpServer)

	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)
	mux.Handle("/mcp", authHandler.AuthMiddleware(httpTransport))

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("listening on port %s: %w", cfg.Port, err)
	}

	addr := listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)

	authHandler.SetBaseURL("http://localhost:" + port)
	printStartupInfo(out, port)

	if addrCh != nil {
		addrCh <- addr
	}

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// readEnvFile parses a .env file into a key-value map.
// Supports KEY=VALUE lines, # comments, and blank lines.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result, scanner.Err()
}
