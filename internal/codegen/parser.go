// Package codegen implements the OpenAPI spec parser and MCP tool generator.
package codegen

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"gopkg.in/yaml.v3"
)

// Parameter represents an OpenAPI operation parameter.
type Parameter struct {
	Name        string
	In          string // "path" or "query"
	Type        string
	Required    bool
	Description string
}

// Operation represents a parsed, non-deprecated OpenAPI operation.
type Operation struct {
	OperationID    string
	Method         string
	Path           string
	Summary        string
	Description    string
	Tags           []string
	Parameters     []Parameter
	RequestBodyRef string
	Scopes         []string
}

// ParsedSpec holds the result of parsing an OpenAPI spec.
type ParsedSpec struct {
	Operations []Operation
}

// Parse parses an OpenAPI 3.0.3 YAML spec, filtering out deprecated operations.
func Parse(data []byte) (*ParsedSpec, error) {
	var spec openAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI spec: %w", err)
	}

	var ops []Operation
	for path, item := range spec.Paths {
		for method, op := range item.methods() {
			if op.Deprecated {
				continue
			}
			ops = append(ops, convertOperation(path, method, op))
		}
	}

	return &ParsedSpec{Operations: ops}, nil
}

// FetchAndParse fetches an OpenAPI spec from a URL and parses it.
func FetchAndParse(ctx context.Context, url string) (*ParsedSpec, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching spec: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	return Parse(data)
}

// Internal YAML types for unmarshalling OpenAPI 3.0.3

type openAPISpec struct {
	Paths map[string]pathItem `yaml:"paths"`
}

type pathItem struct {
	Get    *yamlOperation `yaml:"get"`
	Put    *yamlOperation `yaml:"put"`
	Post   *yamlOperation `yaml:"post"`
	Delete *yamlOperation `yaml:"delete"`
	Patch  *yamlOperation `yaml:"patch"`
}

func (p pathItem) methods() map[string]*yamlOperation {
	m := make(map[string]*yamlOperation)
	if p.Get != nil {
		m["GET"] = p.Get
	}
	if p.Put != nil {
		m["PUT"] = p.Put
	}
	if p.Post != nil {
		m["POST"] = p.Post
	}
	if p.Delete != nil {
		m["DELETE"] = p.Delete
	}
	if p.Patch != nil {
		m["PATCH"] = p.Patch
	}
	return m
}

type yamlOperation struct {
	OperationID string                `yaml:"operationId"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description"`
	Tags        []string              `yaml:"tags"`
	Deprecated  bool                  `yaml:"deprecated"`
	Parameters  []yamlParameter       `yaml:"parameters"`
	RequestBody *yamlRequestBody      `yaml:"requestBody"`
	Security    []map[string][]string `yaml:"security"`
}

type yamlParameter struct {
	Name        string     `yaml:"name"`
	In          string     `yaml:"in"`
	Required    bool       `yaml:"required"`
	Schema      yamlSchema `yaml:"schema"`
	Description string     `yaml:"description"`
}

type yamlSchema struct {
	Type string `yaml:"type"`
	Ref  string `yaml:"$ref"`
}

type yamlRequestBody struct {
	Content map[string]yamlMediaType `yaml:"content"`
}

type yamlMediaType struct {
	Schema yamlSchema `yaml:"schema"`
}

func convertOperation(path, method string, op *yamlOperation) Operation {
	result := Operation{
		OperationID: op.OperationID,
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
	}

	for _, p := range op.Parameters {
		result.Parameters = append(result.Parameters, Parameter{
			Name:        p.Name,
			In:          p.In,
			Type:        p.Schema.Type,
			Required:    p.Required,
			Description: p.Description,
		})
	}

	if op.RequestBody != nil {
		for _, mt := range op.RequestBody.Content {
			if mt.Schema.Ref != "" {
				result.RequestBodyRef = mt.Schema.Ref
				break
			}
		}
	}

	for _, sec := range op.Security {
		for _, scopes := range sec {
			result.Scopes = append(result.Scopes, scopes...)
		}
	}

	return result
}
