package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

type testEnv struct {
	mcpServer    *mcpserver.MCPServer
	mockSpotify  *httptest.Server
	tokenStore   *store.InMemoryTokenStore
	clientID     string
	spotifyToken string

	mu      sync.Mutex
	handler http.HandlerFunc
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		clientID:     "test-client",
		spotifyToken: "spotify-access-token-xyz",
	}

	env.mockSpotify = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		h := env.handler
		env.mu.Unlock()

		if h != nil {
			env.handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
	}))
	t.Cleanup(env.mockSpotify.Close)

	env.tokenStore = store.NewInMemoryTokenStore()
	err := env.tokenStore.Store(context.Background(), env.clientID, &store.TokenRecord{
		SpotifyAccessToken: env.spotifyToken,
	})
	require.NoError(t, err)

	env.mcpServer = mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(false))
	return env
}

func (e *testEnv) authCtx() context.Context {
	return auth.ContextWithClientID(context.Background(), e.clientID)
}

// testRegistrations returns tool registrations that use the typed Spotify client.
// The handlers call the client's underlying HTTP doer directly for simplicity,
// since the test mock doesn't implement specific Spotify API endpoints.
func testRegistrations() []ToolRegistration {
	getTool := mcp.NewTool("get-item",
		mcp.WithDescription("Get an item"),
		mcp.WithString("item_id", mcp.Required(), mcp.Description("The item ID")),
	)
	listTool := mcp.NewTool("list-items",
		mcp.WithDescription("List items"),
	)
	return []ToolRegistration{
		{
			Tool: getTool,
			NewHandler: func(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					id := req.GetString("item_id", "")
					return mcp.NewToolResultText(fmt.Sprintf(`{"id":"%s"}`, id)), nil
				}
			},
		},
		{
			Tool: listTool,
			NewHandler: func(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText(`{"items":[]}`), nil
				}
			},
		},
	}
}

func TestToolRegistryRegistersAllTools(t *testing.T) {
	env := newTestEnv(t)
	regs := testRegistrations()
	Register(env.mcpServer, regs, env.tokenStore, nil, env.mockSpotify.URL, nil)

	tools := env.mcpServer.ListTools()
	assert.Len(t, tools, len(regs))
	assert.NotNil(t, tools["get-item"])
	assert.NotNil(t, tools["list-items"])
}

func TestToolRegistryToolsList(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL, nil)

	tools := env.mcpServer.ListTools()

	getItem := tools["get-item"]
	require.NotNil(t, getItem)
	assert.Equal(t, "get-item", getItem.Tool.Name)
	assert.Equal(t, "Get an item", getItem.Tool.Description)

	listItems := tools["list-items"]
	require.NotNil(t, listItems)
	assert.Equal(t, "list-items", listItems.Tool.Name)
}

func TestToolRegistryToolDispatch(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL, nil)

	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "abc"}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "abc")
}

func TestToolRegistryNonExistentTool(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL, nil)

	tool := env.mcpServer.GetTool("does-not-exist")
	assert.Nil(t, tool)
}

func TestToolRegistrySpotifyError(t *testing.T) {
	env := newTestEnv(t)
	env.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"error":{"status":404,"message":"Not found"}}`)
	}
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL, nil)

	tool := env.mcpServer.GetTool("list-items")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "list-items"
	req.Params.Arguments = map[string]any{}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestSpotifyAPICallLogging(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()

	env := newTestEnv(t)
	// Use a handler factory that actually calls the Spotify mock
	callTool := mcp.NewTool("api-caller",
		mcp.WithDescription("Calls Spotify API"),
	)
	regs := []ToolRegistration{{
		Tool: callTool,
		NewHandler: func(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				resp, err := client.GetPlaylistWithResponse(ctx, "test-id", &spotify.GetPlaylistParams{})
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(string(resp.Body)), nil
			}
		},
	}}
	Register(env.mcpServer, regs, env.tokenStore, nil, env.mockSpotify.URL, logger)

	tool := env.mcpServer.GetTool("api-caller")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "api-caller"
	req.Params.Arguments = map[string]any{}

	_, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)

	// Find the Spotify API log entry
	var foundSpotifyLog bool
	for _, entry := range logs.All() {
		if entry.Message == "spotify api call" {
			foundSpotifyLog = true
			fields := make(map[string]any)
			for _, f := range entry.Context {
				fields[f.Key] = f
			}
			assert.Contains(t, fields, "endpoint")
			assert.Contains(t, fields, "status")
			assert.Contains(t, fields, "duration")
			break
		}
	}
	assert.True(t, foundSpotifyLog, "should log Spotify API calls")
}

func TestToolInvocationLogging(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()

	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL, logger)

	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "playlist-99"}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify log entry
	require.GreaterOrEqual(t, logs.Len(), 1)
	entry := logs.All()[0]
	assert.Equal(t, "tool invocation", entry.Message)

	fields := make(map[string]any)
	for _, f := range entry.Context {
		fields[f.Key] = f
	}
	assert.Contains(t, fields, "tool")
	assert.Contains(t, fields, "args")
	assert.Contains(t, fields, "duration")
	assert.Contains(t, fields, "success")
	assert.Contains(t, fields, "response")
}
