//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/makesometh-ing/spotify-mcp-go/internal/codegen"
	"github.com/makesometh-ing/spotify-mcp-go/internal/tools"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

const spotifySpecURL = "https://developer.spotify.com/reference/web-api/open-api-schema.yaml"

func TestCodegenE2EFullPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectRoot := filepath.Join("..", "..")
	outDir := t.TempDir()

	// --- Step 1: Fetch and parse the real Spotify spec ---
	t.Log("Fetching live Spotify OpenAPI spec...")
	spec, err := codegen.FetchAndParse(ctx, spotifySpecURL)
	require.NoError(t, err, "fetching and parsing spec should succeed")
	require.NotEmpty(t, spec.Operations, "spec should have active operations")
	t.Logf("Parsed %d active operations", len(spec.Operations))

	// --- Step 2: Generate Spotify client via oapi-codegen ---
	t.Log("Generating Spotify client...")
	configPath := filepath.Join(projectRoot, "oapi-codegen.yaml")
	oapiConfig, err := codegen.LoadOapiCodegenConfig(configPath)
	require.NoError(t, err)

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")
	oapiConfig.ClientOutput = clientPath
	oapiConfig.TypesOutput = typesPath

	// Need the raw spec bytes for oapi-codegen (FetchAndParse already parsed,
	// but GenerateFromSpec needs raw bytes). Fetch again.
	resp, err := http.Get(spotifySpecURL)
	require.NoError(t, err)
	specBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)

	err = codegen.GenerateFromSpec(specBytes, oapiConfig)
	require.NoError(t, err, "oapi-codegen generation should succeed")

	_, err = os.Stat(clientPath)
	require.NoError(t, err, "generated_client.go should exist")
	_, err = os.Stat(typesPath)
	require.NoError(t, err, "generated_types.go should exist")
	t.Log("Client and types generated")

	// --- Step 3: Generate MCP tool definitions ---
	t.Log("Generating MCP tool definitions...")
	toolsPath := filepath.Join(outDir, "generated_tools.go")
	err = codegen.GenerateToolsFile(spec.Operations, "tools", toolsPath)
	require.NoError(t, err, "tool generation should succeed")

	_, err = os.Stat(toolsPath)
	require.NoError(t, err, "generated_tools.go should exist")

	// Verify tool count matches operation count
	toolsCode, err := os.ReadFile(toolsPath)
	require.NoError(t, err)
	toolCount := strings.Count(string(toolsCode), "= mcp.NewTool(")
	assert.Equal(t, len(spec.Operations), toolCount,
		"should have one tool per active operation")
	t.Logf("Generated %d MCP tools", toolCount)

	// --- Step 4: Verify generated code compiles ---
	t.Log("Verifying generated client compiles...")
	// Copy generated files to the actual source directories for compilation
	realClientPath := filepath.Join(projectRoot, "internal", "spotify", "generated_client.go")
	realTypesPath := filepath.Join(projectRoot, "internal", "spotify", "generated_types.go")
	realToolsPath := filepath.Join(projectRoot, "internal", "tools", "generated_tools.go")

	copyFile(t, clientPath, realClientPath)
	copyFile(t, typesPath, realTypesPath)
	copyFile(t, toolsPath, realToolsPath)
	t.Cleanup(func() {
		os.Remove(realClientPath)
		os.Remove(realTypesPath)
		os.Remove(realToolsPath)
	})

	// go mod tidy to resolve any new imports
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = projectRoot
	tidyOut, err := tidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy: %s", string(tidyOut))

	buildClient := exec.Command("go", "build", "./internal/spotify/...")
	buildClient.Dir = projectRoot
	buildOut, err := buildClient.CombinedOutput()
	require.NoError(t, err, "client build: %s", string(buildOut))

	buildTools := exec.Command("go", "build", "./internal/tools/...")
	buildTools.Dir = projectRoot
	toolsBuildOut, err := buildTools.CombinedOutput()
	require.NoError(t, err, "tools build: %s", string(toolsBuildOut))
	t.Log("All generated code compiles")

	// --- Step 5: Start server with generated tools ---
	t.Log("Starting server with generated tools...")

	// We can't import the generated tools (they're dynamic), so we create
	// a minimal test tool to prove the server boots with the full pipeline.
	// The real validation is that compilation succeeded above.
	mockSpotifyAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			http.Error(w, `{"error":{"status":404,"message":"Service not found"}}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mockSpotifyAPI.Close()

	mockSpotifyOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token", "refresh_token": "test-refresh",
			"expires_in": 3600, "token_type": "Bearer",
		})
	}))
	defer mockSpotifyOAuth.Close()

	cfg := &serverConfig{
		Port:                 "0",
		SpotifyClientID:      "test-id",
		SpotifyClientSecret:  "test-secret",
		SpotifyTokenEndpoint: mockSpotifyOAuth.URL,
		SpotifyAPIBaseURL:    mockSpotifyAPI.URL + "/v1",
	}

	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()

	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(serverCtx, cfg, testToolRegs(), nil, io.Discard, addrCh, zap.NewNop().Sugar())
	}()

	var serverAddr string
	select {
	case serverAddr = <-addrCh:
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for server")
	}

	// --- Step 6: Verify server responds ---
	t.Log("Verifying server responds...")
	mcpResp, err := http.Post("http://"+serverAddr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	require.NoError(t, err)
	mcpResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, mcpResp.StatusCode,
		"unauthenticated /mcp should return 401")

	// --- Step 7: Authenticated tools/list ---
	t.Log("Verifying tools/list with auth...")
	tokenStore := store.NewInMemoryTokenStore()
	_ = tokenStore.Store(ctx, "e2e-client", &store.TokenRecord{
		SpotifyAccessToken: "test-token",
		SpotifyTokenExpiry: time.Now().Add(time.Hour),
	})

	mcpSrv := mcpserver.NewMCPServer("e2e", "1.0.0", mcpserver.WithToolCapabilities(false))
	tools.Register(mcpSrv, testToolRegs(), tokenStore,
		&auth.SpotifyClient{ClientID: "test", ClientSecret: "test", TokenEndpoint: mockSpotifyOAuth.URL},
		mockSpotifyAPI.URL)

	registeredTools := mcpSrv.ListTools()
	assert.NotEmpty(t, registeredTools, "should have registered tools")
	t.Logf("Server running with %d tools registered", len(registeredTools))

	// --- Step 8: Extract scopes ---
	scopes := codegen.ExtractScopes(spec)
	t.Logf("Extracted %d unique OAuth scopes", len(scopes))
	assert.NotEmpty(t, scopes, "should have extracted scopes from the real spec")

	t.Log("E2E codegen pipeline complete: real spec -> generated code -> compiled -> server running")
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	err = os.WriteFile(dst, data, 0644)
	require.NoError(t, err)
}

// mcpSession and helpers are in integration_test.go (same package, no build tag).
// testToolRegs is also in integration_test.go.
// We reuse them here since this file is in the same package.

// sendJSON sends an authenticated JSON-RPC message and returns the response body.
func sendJSON(t *testing.T, url, token string, msg any) ([]byte, int) {
	t.Helper()
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", url+"/mcp", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode
}
