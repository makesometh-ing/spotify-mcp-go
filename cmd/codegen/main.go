package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/makesometh-ing/spotify-mcp-go/internal/codegen"
)

const defaultSpecURL = "https://developer.spotify.com/reference/web-api/open-api-schema.yaml"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "codegen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	specURL := os.Getenv("SPOTIFY_OPENAPI_SPEC_URL")
	if specURL == "" {
		specURL = defaultSpecURL
	}

	// Resolve output paths relative to the project root (go.mod directory)
	projectRoot := "."
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		// Try parent directories
		for _, candidate := range []string{"..", "../.."} {
			if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil {
				projectRoot = candidate
				break
			}
		}
	}

	configPath := filepath.Join(projectRoot, "oapi-codegen.yaml")
	oapiConfig, err := codegen.LoadOapiCodegenConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading oapi-codegen config: %w", err)
	}
	// Resolve output paths relative to project root
	oapiConfig.ClientOutput = filepath.Join(projectRoot, oapiConfig.ClientOutput)
	oapiConfig.TypesOutput = filepath.Join(projectRoot, oapiConfig.TypesOutput)

	toolsOutput := filepath.Join(projectRoot, "internal", "tools", "generated_tools.go")

	// Step 1: Fetch the spec
	fmt.Printf("Fetching OpenAPI spec from %s...\n", specURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", specURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching spec: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching spec: status %d", resp.StatusCode)
	}
	specData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	// Step 2: Parse and extract operations
	fmt.Println("Parsing spec...")
	spec, err := codegen.Parse(specData)
	if err != nil {
		return fmt.Errorf("parsing spec: %w", err)
	}
	fmt.Printf("  %d active operations (deprecated filtered)\n", len(spec.Operations))

	// Step 3: Extract scopes
	scopes := codegen.ExtractScopes(spec)
	fmt.Printf("  %d unique OAuth scopes\n", len(scopes))

	// Step 4: Generate Spotify client (oapi-codegen)
	fmt.Println("Generating Spotify client...")
	if err := codegen.GenerateFromSpec(specData, oapiConfig); err != nil {
		return fmt.Errorf("generating client: %w", err)
	}
	fmt.Printf("  %s\n", oapiConfig.ClientOutput)
	fmt.Printf("  %s\n", oapiConfig.TypesOutput)

	// Step 5: Generate MCP tool definitions
	fmt.Println("Generating MCP tools...")
	if err := codegen.GenerateToolsFile(spec.Operations, "tools", spec.ServerURL, toolsOutput); err != nil {
		return fmt.Errorf("generating tools: %w", err)
	}
	fmt.Printf("  %s\n", toolsOutput)

	fmt.Println("Done.")
	return nil
}
