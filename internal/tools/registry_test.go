package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

type testEnv struct {
	mcpServer    *mcpserver.MCPServer
	mockSpotify  *httptest.Server
	tokenStore   *store.InMemoryTokenStore
	clientID     string
	spotifyToken string

	mu       sync.Mutex
	requests []*http.Request
	handler  http.HandlerFunc
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		clientID:     "test-client",
		spotifyToken: "spotify-access-token-xyz",
	}

	env.mockSpotify = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		env.requests = append(env.requests, r)
		env.mu.Unlock()

		if env.handler != nil {
			env.handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
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

func (e *testEnv) lastRequest() *http.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) == 0 {
		return nil
	}
	return e.requests[len(e.requests)-1]
}

// testRegistrations returns tool registrations used by the test suite.
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
			NewHandler: func(baseURL string, httpClient *http.Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					id := req.GetString("item_id", "")
					httpReq, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/items/"+id, nil)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					resp, err := httpClient.Do(httpReq)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					if resp.StatusCode >= 400 {
						return mcp.NewToolResultError(fmt.Sprintf("Spotify API error %d: %s", resp.StatusCode, string(body))), nil
					}
					return mcp.NewToolResultText(string(body)), nil
				}
			},
		},
		{
			Tool: listTool,
			NewHandler: func(baseURL string, httpClient *http.Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					httpReq, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/items", nil)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					resp, err := httpClient.Do(httpReq)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					if resp.StatusCode >= 400 {
						return mcp.NewToolResultError(fmt.Sprintf("Spotify API error %d: %s", resp.StatusCode, string(body))), nil
					}
					return mcp.NewToolResultText(string(body)), nil
				}
			},
		},
	}
}

func TestToolRegistryRegistersAllTools(t *testing.T) {
	env := newTestEnv(t)
	regs := testRegistrations()
	Register(env.mcpServer, regs, env.tokenStore, nil, env.mockSpotify.URL)

	tools := env.mcpServer.ListTools()
	assert.Len(t, tools, len(regs))
	assert.NotNil(t, tools["get-item"])
	assert.NotNil(t, tools["list-items"])
}

func TestToolRegistryToolsList(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	tools := env.mcpServer.ListTools()

	// Verify names and descriptions are present
	getItem := tools["get-item"]
	require.NotNil(t, getItem)
	assert.Equal(t, "get-item", getItem.Tool.Name)
	assert.Equal(t, "Get an item", getItem.Tool.Description)

	listItems := tools["list-items"]
	require.NotNil(t, listItems)
	assert.Equal(t, "list-items", listItems.Tool.Name)
	assert.Equal(t, "List items", listItems.Tool.Description)
}

func TestToolRegistryToolDispatch(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	// Invoke get-item; verify the mock receives a request to /v1/items/abc
	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "abc"}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	lastReq := env.lastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "/v1/items/abc", lastReq.URL.Path)
}

func TestToolRegistryHandlerReceivesToken(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "xyz"}

	_, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)

	lastReq := env.lastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "Bearer "+env.spotifyToken, lastReq.Header.Get("Authorization"))
}

func TestToolRegistryCallsCorrectEndpoint(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	// Call list-items; verify GET /v1/items
	tool := env.mcpServer.GetTool("list-items")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "list-items"
	req.Params.Arguments = map[string]any{}

	_, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)

	lastReq := env.lastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "GET", lastReq.Method)
	assert.Equal(t, "/v1/items", lastReq.URL.Path)
}

func TestToolRegistryReturnsAPIResponse(t *testing.T) {
	env := newTestEnv(t)
	env.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": "Test Playlist"})
	}
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "123"}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	// The result text should contain the mock response body
	require.NotEmpty(t, result.Content)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "Test Playlist")
}

func TestToolRegistryNonExistentTool(t *testing.T) {
	env := newTestEnv(t)
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	tool := env.mcpServer.GetTool("does-not-exist")
	assert.Nil(t, tool, "non-existent tool should return nil")
}

func TestToolRegistrySpotifyError(t *testing.T) {
	env := newTestEnv(t)
	env.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":{"status":404,"message":"Not found"}}`)
	}
	Register(env.mcpServer, testRegistrations(), env.tokenStore, nil, env.mockSpotify.URL)

	tool := env.mcpServer.GetTool("get-item")
	require.NotNil(t, tool)

	req := mcp.CallToolRequest{}
	req.Params.Name = "get-item"
	req.Params.Arguments = map[string]any{"item_id": "nonexistent"}

	result, err := tool.Handler(env.authCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Spotify 404 should produce an error result")

	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "404")
	assert.Contains(t, text, "Not found")
}
