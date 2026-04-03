package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	oapicodegen "github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
	"gopkg.in/yaml.v3"
)

// GenerateConfig holds the configuration for oapi-codegen generation.
type GenerateConfig struct {
	PackageName  string
	ClientOutput string
	TypesOutput  string
	SkipPrune    bool
}

// LoadOapiCodegenConfig reads an oapi-codegen YAML config file and returns
// the settings relevant to generation. The client output path comes from the
// config's "output" field. The types output path is derived by replacing
// "_client.go" with "_types.go".
func LoadOapiCodegenConfig(path string) (*GenerateConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading oapi-codegen config: %w", err)
	}

	var raw struct {
		Package       string `yaml:"package"`
		Output        string `yaml:"output"`
		OutputOptions struct {
			SkipPrune bool `yaml:"skip-prune"`
		} `yaml:"output-options"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing oapi-codegen config: %w", err)
	}

	clientOutput := raw.Output
	typesOutput := strings.Replace(clientOutput, "_client.go", "_types.go", 1)

	return &GenerateConfig{
		PackageName:  raw.Package,
		ClientOutput: clientOutput,
		TypesOutput:  typesOutput,
		SkipPrune:    raw.OutputOptions.SkipPrune,
	}, nil
}

// GenerateFromSpec generates Go client and types files from an OpenAPI spec.
// It filters out deprecated operations before passing the spec to oapi-codegen.
func GenerateFromSpec(specData []byte, config *GenerateConfig) error {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		return fmt.Errorf("loading OpenAPI spec: %w", err)
	}

	filterDeprecated(doc)

	clientCode, err := generateCode(doc, config.PackageName, config.SkipPrune, true, false)
	if err != nil {
		return fmt.Errorf("generating client code: %w", err)
	}

	typesCode, err := generateCode(doc, config.PackageName, config.SkipPrune, false, true)
	if err != nil {
		return fmt.Errorf("generating types code: %w", err)
	}

	if err := writeFile(config.ClientOutput, clientCode); err != nil {
		return fmt.Errorf("writing client file: %w", err)
	}
	if err := writeFile(config.TypesOutput, typesCode); err != nil {
		return fmt.Errorf("writing types file: %w", err)
	}

	return nil
}

func generateCode(doc *openapi3.T, pkg string, skipPrune, client, models bool) (string, error) {
	cfg := oapicodegen.Configuration{
		PackageName: pkg,
		Generate: oapicodegen.GenerateOptions{
			Client: client,
			Models: models,
		},
		OutputOptions: oapicodegen.OutputOptions{
			SkipPrune: skipPrune,
		},
	}
	code, err := oapicodegen.Generate(doc, cfg)
	if err != nil {
		return "", err
	}
	return code, nil
}

func filterDeprecated(doc *openapi3.T) {
	if doc.Paths == nil {
		return
	}

	var toDelete []string

	for path, item := range doc.Paths.Map() {
		if item.Get != nil && item.Get.Deprecated {
			item.Get = nil
		}
		if item.Put != nil && item.Put.Deprecated {
			item.Put = nil
		}
		if item.Post != nil && item.Post.Deprecated {
			item.Post = nil
		}
		if item.Delete != nil && item.Delete.Deprecated {
			item.Delete = nil
		}
		if item.Patch != nil && item.Patch.Deprecated {
			item.Patch = nil
		}

		if item.Get == nil && item.Put == nil && item.Post == nil &&
			item.Delete == nil && item.Patch == nil {
			toDelete = append(toDelete, path)
		}
	}

	for _, path := range toDelete {
		doc.Paths.Delete(path)
	}
}

func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
