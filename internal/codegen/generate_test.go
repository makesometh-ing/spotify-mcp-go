package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOapiCodegenGeneratesFiles(t *testing.T) {
	fixture := loadFixture(t)
	outDir := t.TempDir()

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")

	err := GenerateFromSpec(fixture, &GenerateConfig{
		PackageName:  "spotify",
		ClientOutput: clientPath,
		TypesOutput:  typesPath,
		SkipPrune:    true,
	})
	require.NoError(t, err)

	_, err = os.Stat(clientPath)
	assert.NoError(t, err, "generated_client.go should exist")

	_, err = os.Stat(typesPath)
	assert.NoError(t, err, "generated_types.go should exist")
}

func TestOapiCodegenClientHasMethodPerOperation(t *testing.T) {
	fixture := loadFixture(t)
	outDir := t.TempDir()

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")

	err := GenerateFromSpec(fixture, &GenerateConfig{
		PackageName:  "spotify",
		ClientOutput: clientPath,
		TypesOutput:  typesPath,
		SkipPrune:    true,
	})
	require.NoError(t, err)

	clientCode, err := os.ReadFile(clientPath)
	require.NoError(t, err)
	code := string(clientCode)

	// Each active operation should produce a client method.
	// oapi-codegen converts kebab-case operationId to CamelCase.
	assert.Contains(t, code, "GetPlaylist")
	assert.Contains(t, code, "AddTracksToPlaylist")
	assert.Contains(t, code, "GetPlaybackState")
}

func TestOapiCodegenTypesIncludeSchemas(t *testing.T) {
	fixture := loadFixture(t)
	outDir := t.TempDir()

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")

	err := GenerateFromSpec(fixture, &GenerateConfig{
		PackageName:  "spotify",
		ClientOutput: clientPath,
		TypesOutput:  typesPath,
		SkipPrune:    true,
	})
	require.NoError(t, err)

	typesCode, err := os.ReadFile(typesPath)
	require.NoError(t, err)
	code := string(typesCode)

	// The fixture has AddTracksRequest in components/schemas, referenced by
	// the active add-tracks-to-playlist operation.
	assert.Contains(t, code, "AddTracksRequest")
}

func TestOapiCodegenExcludesDeprecated(t *testing.T) {
	fixture := loadFixture(t)
	outDir := t.TempDir()

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")

	err := GenerateFromSpec(fixture, &GenerateConfig{
		PackageName:  "spotify",
		ClientOutput: clientPath,
		TypesOutput:  typesPath,
		SkipPrune:    true,
	})
	require.NoError(t, err)

	clientCode, err := os.ReadFile(clientPath)
	require.NoError(t, err)
	code := string(clientCode)

	// Deprecated operations: transfer-playback -> TransferPlayback, search -> Search.
	// Neither should appear as client methods.
	assert.False(t, strings.Contains(code, "TransferPlayback"),
		"deprecated transfer-playback should not appear in generated client")

	// Check that the search operation (operationId "search") is not generated.
	// Use a function signature pattern to avoid false positives.
	assert.False(t, strings.Contains(code, "func (c *ClientWithResponses) SearchWithResponse"),
		"deprecated search should not appear in generated client")
	assert.False(t, strings.Contains(code, "func (c *Client) Search("),
		"deprecated search should not appear in generated client")
}

func TestOapiCodegenDeterministic(t *testing.T) {
	fixture := loadFixture(t)

	generate := func() (string, string) {
		outDir := t.TempDir()
		clientPath := filepath.Join(outDir, "generated_client.go")
		typesPath := filepath.Join(outDir, "generated_types.go")

		err := GenerateFromSpec(fixture, &GenerateConfig{
			PackageName:  "spotify",
			ClientOutput: clientPath,
			TypesOutput:  typesPath,
			SkipPrune:    true,
		})
		require.NoError(t, err)

		client, err := os.ReadFile(clientPath)
		require.NoError(t, err)
		types, err := os.ReadFile(typesPath)
		require.NoError(t, err)

		return string(client), string(types)
	}

	client1, types1 := generate()
	client2, types2 := generate()

	assert.Equal(t, client1, client2, "client code should be deterministic across runs")
	assert.Equal(t, types1, types2, "types code should be deterministic across runs")
}

func TestOapiCodegenRespectsConfig(t *testing.T) {
	fixture := loadFixture(t)
	outDir := t.TempDir()

	clientPath := filepath.Join(outDir, "generated_client.go")
	typesPath := filepath.Join(outDir, "generated_types.go")

	// Load config from the project's oapi-codegen.yaml
	configPath := filepath.Join("..", "..", "oapi-codegen.yaml")
	config, err := LoadOapiCodegenConfig(configPath)
	require.NoError(t, err)

	// Override output paths to use temp dir
	config.ClientOutput = clientPath
	config.TypesOutput = typesPath

	err = GenerateFromSpec(fixture, config)
	require.NoError(t, err)

	// Config says package: spotify
	clientCode, err := os.ReadFile(clientPath)
	require.NoError(t, err)
	assert.Contains(t, string(clientCode), "package spotify")

	typesCode, err := os.ReadFile(typesPath)
	require.NoError(t, err)
	assert.Contains(t, string(typesCode), "package spotify")

	// Config says skip-prune: true, so unreferenced schemas should still appear.
	// TransferPlaybackRequest is defined in components/schemas but only referenced
	// by the deprecated transfer-playback operation. With skip-prune, it's kept.
	assert.Contains(t, string(typesCode), "TransferPlaybackRequest")
}
