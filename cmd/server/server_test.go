package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
		errCh <- run(ctx, cfg, nil, nil, &buf, addrCh, zap.NewNop().Sugar())
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
	printStartupInfo(&buf, "8080", "")
	assert.Contains(t, buf.String(), "http://127.0.0.1:8080/mcp")
}

func TestServerStartupOutputCallbackURL(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080", "")
	assert.Contains(t, buf.String(), "http://127.0.0.1:8080/callback")
}

func TestServerStartupOutputDashboardInstructions(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080", "")
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

func TestNewLoggerDebugFalseReturnsNop(t *testing.T) {
	logger := newLogger(false)
	defer logger.Sync()
	// A nop logger's Core is always disabled at every level
	assert.False(t, logger.Desugar().Core().Enabled(zap.DebugLevel),
		"nop logger should not be enabled at debug level")
}

func TestNewLoggerDebugTrueReturnsEnabled(t *testing.T) {
	logger := newLogger(true)
	defer logger.Sync()
	assert.True(t, logger.Desugar().Core().Enabled(zap.DebugLevel),
		"debug logger should be enabled at debug level")
}

func TestHTTPLoggingMiddleware(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := httpLoggingMiddleware(logger, inner)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/test-path")
	require.NoError(t, err)
	resp.Body.Close()

	require.GreaterOrEqual(t, logs.Len(), 1)
	entry := logs.All()[0]
	assert.Equal(t, "http request", entry.Message)

	fields := make(map[string]any)
	for _, f := range entry.Context {
		fields[f.Key] = f
	}
	assert.Contains(t, fields, "method")
	assert.Contains(t, fields, "path")
	assert.Contains(t, fields, "status")
	assert.Contains(t, fields, "duration")
}

func TestBaseURLConfigDefault(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "test-id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "test-secret")
	t.Setenv("SPOTIFY_MCP_BASE_URL", "")

	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.Empty(t, cfg.BaseURL, "base URL should be empty when not configured")
}

func TestBaseURLConfigSet(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "test-id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "test-secret")
	t.Setenv("SPOTIFY_MCP_BASE_URL", "https://spotify-mcp.example.com")

	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.Equal(t, "https://spotify-mcp.example.com", cfg.BaseURL)
}

func TestBaseURLUsedInWellKnown(t *testing.T) {
	cfg := validConfig(t)
	cfg.BaseURL = "https://spotify-mcp.example.com"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, nil, nil, &buf, addrCh, zap.NewNop().Sugar())
	}()

	select {
	case addr := <-addrCh:
		// Well-known metadata should use configured base URL
		resp, err := http.Get("http://" + addr + "/.well-known/oauth-protected-resource")
		require.NoError(t, err)
		defer resp.Body.Close()

		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		assert.Equal(t, "https://spotify-mcp.example.com", body["resource"])

		servers, ok := body["authorization_servers"].([]any)
		require.True(t, ok)
		assert.Equal(t, "https://spotify-mcp.example.com", servers[0])
	case err := <-errCh:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server")
	}
}

func TestBaseURLDefaultFallback(t *testing.T) {
	cfg := validConfig(t)
	// BaseURL empty = default to 127.0.0.1:<port>

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cfg, nil, nil, &buf, addrCh, zap.NewNop().Sugar())
	}()

	select {
	case addr := <-addrCh:
		resp, err := http.Get("http://" + addr + "/.well-known/oauth-protected-resource")
		require.NoError(t, err)
		defer resp.Body.Close()

		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		// Should default to http://127.0.0.1:<port>
		assert.Contains(t, body["resource"].(string), "http://127.0.0.1:")
	case err := <-errCh:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server")
	}
}

func TestBaseURLStartupOutput(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080", "https://spotify-mcp.example.com")
	output := buf.String()
	assert.Contains(t, output, "https://spotify-mcp.example.com/mcp")
	assert.Contains(t, output, "https://spotify-mcp.example.com/callback")
}

func TestBaseURLStartupOutputDefault(t *testing.T) {
	var buf bytes.Buffer
	printStartupInfo(&buf, "8080", "")
	output := buf.String()
	assert.Contains(t, output, "http://127.0.0.1:8080/mcp")
	assert.Contains(t, output, "http://127.0.0.1:8080/callback")
}

func TestSpotifyAPIBaseURLDefault(t *testing.T) {
	assert.Equal(t, "https://api.spotify.com/v1", defaultSpotifyAPIBaseURL)
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
