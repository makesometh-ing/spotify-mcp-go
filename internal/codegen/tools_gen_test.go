package codegen

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseFixtureOps(t *testing.T) []Operation {
	t.Helper()
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)
	return spec.Operations
}

func TestToolGenGeneratesOneToolPerOperation(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// 3 active operations in the fixture, so 3 tool definitions
	count := strings.Count(code, "= mcp.NewTool(")
	assert.Equal(t, 3, count)
}

func TestToolGenToolNames(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Tool names must match operationId directly
	assert.Contains(t, code, `mcp.NewTool("get-playlist"`)
	assert.Contains(t, code, `mcp.NewTool("add-tracks-to-playlist"`)
	assert.Contains(t, code, `mcp.NewTool("get-playback-state"`)
}

func TestToolGenToolDescriptions(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Descriptions come from the OpenAPI summary field
	assert.Contains(t, code, "Get Playlist")
	assert.Contains(t, code, "Add Items to Playlist")
	assert.Contains(t, code, "Get Playback State")

	// Description text (from OpenAPI description) should also appear
	assert.Contains(t, code, "Get a playlist owned by a Spotify user.")
}

func TestToolGenRequiredPathParams(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// playlist_id is a required path parameter in get-playlist and add-tracks-to-playlist.
	// It should appear with mcp.Required().
	assert.Contains(t, code, `mcp.WithString("playlist_id", mcp.Required()`)
}

func TestToolGenQueryParamTypes(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// market and fields are string query params
	assert.Contains(t, code, `mcp.WithString("market"`)
	assert.Contains(t, code, `mcp.WithString("fields"`)
	assert.Contains(t, code, `mcp.WithString("additional_types"`)
}

func TestToolGenOptionalParamsNotRequired(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// market is optional. Find the market WithString call and verify it does NOT
	// have Required() in the same call.
	// We check that "market", mcp.Required() does NOT appear (market is optional).
	assert.NotContains(t, code, `"market", mcp.Required()`)
	assert.NotContains(t, code, `"fields", mcp.Required()`)
	assert.NotContains(t, code, `"additional_types", mcp.Required()`)
}

func TestToolGenParamDescriptions(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Parameter descriptions from the OpenAPI spec
	assert.Contains(t, code, "The Spotify ID of the playlist.")
	assert.Contains(t, code, "An ISO 3166-1 alpha-2 country code.")
	assert.Contains(t, code, "Filters for the query.")
	assert.Contains(t, code, "A comma-separated list of item types.")
}

func TestToolGenHandlerSignatures(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Each tool has a handler factory returning the correct mcp-go handler signature
	assert.Contains(t, code, "func NewGetPlaylistHandler(")
	assert.Contains(t, code, "func NewAddTracksToPlaylistHandler(")
	assert.Contains(t, code, "func NewGetPlaybackStateHandler(")

	// The handlers return the correct function type
	assert.Contains(t, code, "mcp.CallToolRequest")
	assert.Contains(t, code, "*mcp.CallToolResult")
}

func TestToolGenDeterministic(t *testing.T) {
	ops := parseFixtureOps(t)

	code1, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	code2, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	assert.Equal(t, code1, code2, "generated tools code should be deterministic")
}

func TestToolGenScopes(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Scopes should be embedded per tool as comments or variables
	assert.Contains(t, code, "playlist-read-private")
	assert.Contains(t, code, "playlist-modify-public")
	assert.Contains(t, code, "playlist-modify-private")
	assert.Contains(t, code, "user-read-playback-state")
}

func TestToolGenAllScopesFunction(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// AllScopes() must exist and return deduplicated, sorted scopes
	assert.Contains(t, code, "func AllScopes() []string")
	assert.Contains(t, code, "sort.Strings")
}

func TestToolGenServerURLConstant(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	code, err := GenerateTools(spec.Operations, "tools", spec.ServerURL)
	require.NoError(t, err)

	assert.Contains(t, code, `const ServerURL = "https://api.spotify.com/v1"`)
}

func TestToolGenServerURLEmpty(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	code, err := GenerateTools(spec.Operations, "tools", "")
	require.NoError(t, err)

	assert.NotContains(t, code, "const ServerURL")
}

func TestToolGenExcludesDeprecated(t *testing.T) {
	ops := parseFixtureOps(t)
	code, err := GenerateTools(ops, "tools", "")
	require.NoError(t, err)

	// Deprecated operations: transfer-playback, search
	assert.NotContains(t, code, "transfer-playback")
	assert.NotContains(t, code, `mcp.NewTool("search"`)
	assert.NotContains(t, code, "TransferPlayback")
}

func TestToolGenBuildIntegration(t *testing.T) {
	fixture := loadFixture(t)
	projectRoot := filepath.Join("..", "..")

	// Generate Spotify client from fixture (needed by tools file)
	configPath := filepath.Join(projectRoot, "oapi-codegen.yaml")
	oapiConfig, err := LoadOapiCodegenConfig(configPath)
	require.NoError(t, err)
	clientPath := filepath.Join(projectRoot, oapiConfig.ClientOutput)
	typesPath := filepath.Join(projectRoot, oapiConfig.TypesOutput)
	backupFile(t, clientPath)
	backupFile(t, typesPath)
	err = GenerateFromSpec(fixture, &GenerateConfig{
		PackageName:  oapiConfig.PackageName,
		ClientOutput: clientPath,
		TypesOutput:  typesPath,
		SkipPrune:    oapiConfig.SkipPrune,
	})
	require.NoError(t, err)

	// Parse fixture for tool generation
	spec, err := Parse(fixture)
	require.NoError(t, err)

	// Generate tools file
	toolsPath := filepath.Join(projectRoot, "internal", "tools", "generated_tools.go")
	backupFile(t, toolsPath)
	err = GenerateToolsFile(spec.Operations, "tools", spec.ServerURL, toolsPath)
	require.NoError(t, err)

	// go mod tidy to resolve any new imports
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = projectRoot
	tidyOut, err := tidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy should succeed: %s", string(tidyOut))

	// Verify go build succeeds
	cmd := exec.Command("go", "build", "./internal/tools/...")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build should succeed: %s", string(output))
}
