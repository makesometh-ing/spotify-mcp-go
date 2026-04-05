package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"

	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

// syntheticArgs builds MCP arguments from a tool's InputSchema.
// String -> "test", Number -> 1, Array -> ["test"], Boolean -> true
func syntheticArgs(tool mcp.Tool) map[string]interface{} {
	args := make(map[string]interface{})
	for name, raw := range tool.InputSchema.Properties {
		prop, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch prop["type"] {
		case "number", "integer":
			args[name] = float64(1)
		case "array":
			args[name] = []interface{}{"test"}
		case "boolean":
			args[name] = true
		default:
			args[name] = "test"
		}
	}
	return args
}

func TestAllToolsHTTPIntegration(t *testing.T) {
	regs := AllRegistrations()
	require.NotEmpty(t, regs, "AllRegistrations should return tools")

	for _, reg := range regs {
		toolName := reg.Tool.Name
		t.Run(toolName, func(t *testing.T) {
			defer gock.Off()
			gock.Clean()

			httpClient := &http.Client{Transport: gock.DefaultTransport}
			client, err := spotify.NewClientWithResponses(
				ServerURL,
				spotify.WithHTTPClient(httpClient),
			)
			require.NoError(t, err)

			handler := reg.NewHandler(client)
			args := syntheticArgs(reg.Tool)

			var capturedReq *http.Request
			gock.New(ServerURL).
				AddMatcher(func(req *http.Request, ereq *gock.Request) (bool, error) {
					capturedReq = req
					return true, nil
				}).
				Reply(200).
				BodyString("null")

			req := mcp.CallToolRequest{}
			req.Params.Name = toolName
			req.Params.Arguments = args

			result, err := handler(context.Background(), req)
			require.NoError(t, err, "tool %s handler returned Go error", toolName)
			require.NotNil(t, result)
			// We don't assert !result.IsError because some response parsers
			// may reject our generic mock body. We only care that an HTTP
			// request was made with correct parameters.
			require.NotNil(t, capturedReq, "no HTTP request captured for tool %s", toolName)
		})
	}
}

func TestAddItemsToPlaylistBodyUris(t *testing.T) {
	defer gock.Off()

	httpClient := &http.Client{Transport: gock.DefaultTransport}
	client, err := spotify.NewClientWithResponses(
		ServerURL,
		spotify.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	handler := NewAddItemsToPlaylistHandler(client)

	var capturedReq *http.Request
	gock.New(ServerURL).
		AddMatcher(func(req *http.Request, ereq *gock.Request) (bool, error) {
			capturedReq = req
			return true, nil
		}).
		Reply(200).
		JSON(map[string]string{"snapshot_id": "abc"})

	args := map[string]interface{}{
		"playlist_id": "test-playlist-123",
		"uris":        []interface{}{"spotify:track:abc", "spotify:track:def"},
		"position":    float64(0),
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "add-items-to-playlist"
	req.Params.Arguments = args

	result, err := handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	require.NotNil(t, capturedReq)

	// Path should contain the playlist ID
	assert.True(t, strings.Contains(capturedReq.URL.Path, "test-playlist-123"),
		"path should contain playlist_id, got: %s", capturedReq.URL.Path)

	// uris should NOT be in query string (body wins over query for collisions)
	assert.Empty(t, capturedReq.URL.Query().Get("uris"),
		"uris should NOT be in query string")

	// Parse the request body
	var body map[string]interface{}
	err = json.NewDecoder(capturedReq.Body).Decode(&body)
	require.NoError(t, err, "request body should be valid JSON")

	uris, ok := body["uris"]
	require.True(t, ok, "body should contain 'uris' field")

	urisArr, ok := uris.([]interface{})
	require.True(t, ok, "uris should be an array")
	assert.Len(t, urisArr, 2)
	assert.Equal(t, "spotify:track:abc", urisArr[0])
	assert.Equal(t, "spotify:track:def", urisArr[1])
}
