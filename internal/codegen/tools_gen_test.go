package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToolFiles(t *testing.T) {
	clientSrc := loadTestFixture(t, "fixture_client.go")
	typesSrc := loadTestFixture(t, "fixture_types.go")
	specData := loadTestFixture(t, "spotify_fixture.yaml")

	inspect, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	meta, err := ExtractMetadata(specData)
	require.NoError(t, err)

	tools := MergeToolData(inspect, meta)
	require.NotEmpty(t, tools)

	outDir := t.TempDir()
	err = GenerateToolFiles(tools, "tools", meta.ServerURL, outDir)
	require.NoError(t, err)

	t.Run("per-endpoint files created", func(t *testing.T) {
		for _, td := range tools {
			filename := "generated_tool_" + snakeCase(td.OperationID) + ".go"
			path := filepath.Join(outDir, filename)
			_, err := os.Stat(path)
			assert.NoError(t, err, "missing file: %s", filename)
		}
	})

	t.Run("aggregator file created", func(t *testing.T) {
		path := filepath.Join(outDir, "generated_tools_all.go")
		_, err := os.Stat(path)
		assert.NoError(t, err)
	})

	t.Run("get-playlist tool file", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_get_playlist.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "var GetPlaylistTool = mcp.NewTool(")
		assert.Contains(t, code, `"get-playlist"`)
		assert.Contains(t, code, "func NewGetPlaylistHandler(")
		assert.Contains(t, code, "GetPlaylistToolScopes")
		assert.Contains(t, code, `mcp.WithString("playlist_id"`)
		assert.Contains(t, code, "mcp.Required()")
		assert.Contains(t, code, "params := &spotify.GetPlaylistParams{}")
	})

	t.Run("create-playlist has typed body", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_create_playlist.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "var body spotify.CreatePlaylistJSONRequestBody")
		assert.NotContains(t, code, "map[string]interface{}")
		assert.Contains(t, code, "body.Name")
	})

	t.Run("aggregator contents", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tools_all.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "func AllRegistrations()")
		assert.Contains(t, code, "func AllScopes()")
		assert.Contains(t, code, "var AllTools")
		assert.Contains(t, code, "func toStringSlice(")
		assert.Contains(t, code, "func toInt(")
		assert.Contains(t, code, `const ServerURL = "https://api.spotify.com/v1"`)
	})

	t.Run("no duplicate param declarations", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_search.go"))
		require.NoError(t, err)
		code := string(data)

		count := strings.Count(code, `mcp.WithString("q"`)
		assert.Equal(t, 1, count, "param 'q' should appear exactly once")
	})
}

func TestSnakeCase(t *testing.T) {
	assert.Equal(t, "add_items_to_playlist", snakeCase("add-items-to-playlist"))
	assert.Equal(t, "search", snakeCase("search"))
}

func TestHelperFunctions(t *testing.T) {
	t.Run("kebabToCamel", func(t *testing.T) {
		assert.Equal(t, "AddItemsToPlaylist", kebabToCamel("add-items-to-playlist"))
		assert.Equal(t, "Search", kebabToCamel("search"))
	})

	t.Run("snakeToCamel", func(t *testing.T) {
		assert.Equal(t, "playlistId", snakeToCamel("playlist_id"))
		assert.Equal(t, "typeParam", snakeToCamel("type"))
	})

	t.Run("snakeToPascal", func(t *testing.T) {
		assert.Equal(t, "PlaylistId", snakeToPascal("playlist_id"))
	})

	t.Run("toolDescription", func(t *testing.T) {
		assert.Equal(t, "Summary\n\nDescription", toolDescription("Summary", "Description"))
		assert.Equal(t, "Summary", toolDescription("Summary", ""))
		assert.Equal(t, "Description", toolDescription("", "Description"))
	})
}
