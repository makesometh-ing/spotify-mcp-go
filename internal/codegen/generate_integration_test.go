package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOapiCodegenBuildIntegration generates files to the actual internal/spotify/
// directory and verifies they compile with `go build`.
func TestOapiCodegenBuildIntegration(t *testing.T) {
	fixture := loadFixture(t)

	projectRoot := filepath.Join("..", "..")
	config, err := LoadOapiCodegenConfig(filepath.Join(projectRoot, "oapi-codegen.yaml"))
	require.NoError(t, err)

	// Resolve output paths relative to project root
	clientPath := filepath.Join(projectRoot, config.ClientOutput)
	typesPath := filepath.Join(projectRoot, config.TypesOutput)
	config.ClientOutput = clientPath
	config.TypesOutput = typesPath

	// Backup and restore generated files after test
	backupFile(t, clientPath)
	backupFile(t, typesPath)

	err = GenerateFromSpec(fixture, config)
	require.NoError(t, err)

	// Verify generated files exist
	_, err = os.Stat(clientPath)
	require.NoError(t, err, "generated_client.go should exist")
	_, err = os.Stat(typesPath)
	require.NoError(t, err, "generated_types.go should exist")

	// The generated files import github.com/oapi-codegen/runtime which may not
	// be in go.mod yet (it gets added when generated files are committed). Run
	// go mod tidy to resolve imports before building.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = projectRoot
	tidyOut, err := tidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy should succeed: %s", string(tidyOut))

	// Verify go build succeeds
	cmd := exec.Command("go", "build", "./internal/spotify/...")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build should succeed: %s", string(output))
}
