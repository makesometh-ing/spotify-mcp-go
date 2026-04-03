package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validConfig(t *testing.T) *serverConfig {
	t.Helper()
	return &serverConfig{
		Port:                "0", // random port
		SpotifyClientID:     "test-client-id",
		SpotifyClientSecret: "test-client-secret",
		TokenDBPath:         filepath.Join(t.TempDir(), "tokens.db"),
	}
}

func TestServerStartupListensOnPort(t *testing.T) {
	cfg := validConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, nil, &buf, addrCh)
	}()

	select {
	case addr := <-addrCh:
		// POST /mcp without auth should return 401
		resp, err := http.Post("http://"+addr+"/mcp", "application/json",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	case err := <-errCh:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}
}

func TestServerStartupOutputMCPEndpoint(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080")
	assert.Contains(t, buf.String(), "http://localhost:8080/mcp")
}

func TestServerStartupOutputCallbackURL(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080")
	assert.Contains(t, buf.String(), "http://localhost:8080/callback")
}

func TestServerStartupOutputDashboardInstructions(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080")
	output := buf.String()
	assert.Contains(t, output, "Spotify Developer Dashboard")
	assert.Contains(t, output, "https://developer.spotify.com/dashboard")
}

func TestServerStartupMissingClientID(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "test-secret")
	_, err := loadConfig("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SPOTIFY_CLIENT_ID")
}

func TestServerStartupMissingClientSecret(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "test-id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")
	_, err := loadConfig("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SPOTIFY_CLIENT_SECRET")
}

func TestServerStartupEnvFile(t *testing.T) {
	// Clear env vars so .env file values are used
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")

	envFile := filepath.Join(t.TempDir(), ".env")
	err := os.WriteFile(envFile, []byte("SPOTIFY_CLIENT_ID=from-file\nSPOTIFY_CLIENT_SECRET=secret-from-file\n"), 0644)
	require.NoError(t, err)

	cfg, err := loadConfig(envFile)
	require.NoError(t, err)
	assert.Equal(t, "from-file", cfg.SpotifyClientID)
	assert.Equal(t, "secret-from-file", cfg.SpotifyClientSecret)

	// Env vars take precedence over .env file
	t.Setenv("SPOTIFY_CLIENT_ID", "from-env")
	cfg, err = loadConfig(envFile)
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.SpotifyClientID)
}

func TestServerStartupTokenDBOverride(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "test-id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "test-secret")

	// Default path
	t.Setenv("SPOTIFY_MCP_TOKEN_DB", "")
	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.Contains(t, cfg.TokenDBPath, "tokens.db")

	// Custom path
	t.Setenv("SPOTIFY_MCP_TOKEN_DB", "/tmp/custom-tokens.db")
	cfg, err = loadConfig("")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom-tokens.db", cfg.TokenDBPath)
}
