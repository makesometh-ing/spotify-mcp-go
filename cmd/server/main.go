package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/makesometh-ing/spotify-mcp-go/internal/tools"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging to stderr")
	spotifyClientID := flag.String("spotify-client-id", "", "Spotify app client ID (overrides SPOTIFY_CLIENT_ID env var)")
	spotifyClientSecret := flag.String("spotify-client-secret", "", "Spotify app client secret (overrides SPOTIFY_CLIENT_SECRET env var)")
	port := flag.String("port", "", "HTTP server port (overrides SPOTIFY_MCP_PORT env var, default 8080)")
	tokenDB := flag.String("token-db", "", "SQLite token storage path (overrides SPOTIFY_MCP_TOKEN_DB env var)")
	baseURL := flag.String("base-url", "", "Public base URL for reverse proxy/tunnel (overrides SPOTIFY_MCP_BASE_URL env var)")
	flag.Parse()

	logger := newLogger(*debug)
	defer func() { _ = logger.Sync() }()

	flags := &flagOverrides{
		SpotifyClientID:     *spotifyClientID,
		SpotifyClientSecret: *spotifyClientSecret,
		Port:                *port,
		TokenDBPath:         *tokenDB,
		BaseURL:             *baseURL,
	}

	cfg, err := loadConfig(".env", flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, tools.AllRegistrations(), tools.AllScopes(), os.Stdout, nil, logger); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
