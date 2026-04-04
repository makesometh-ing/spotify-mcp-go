// Package codegen implements the OpenAPI spec parser and MCP tool generator.
package codegen

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parameter represents an OpenAPI operation parameter.
type Parameter struct {
	Name        string
	In          string // "path" or "query"
	Type        string
	Required    bool
	Description string
	HasEnum     bool
}

// Operation represents a parsed, non-deprecated OpenAPI operation.
type Operation struct {
	OperationID     string
	Method          string
	Path            string
	Summary         string
	Description     string
	Tags            []string
	Parameters      []Parameter
	RequestBodyRef  string
	BodyContentType string // e.g., "application/json", "image/jpeg"
	Scopes          []string
}

// ParsedSpec holds the result of parsing an OpenAPI spec.
type ParsedSpec struct {
	Operations []Operation
	ServerURL  string // from servers[0].url, if present
}

// Parse parses an OpenAPI 3.0.3 YAML spec, filtering out deprecated operations.
func Parse(data []byte) (*ParsedSpec, error) {
	var spec openAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI spec: %w", err)
	}

	var serverURL string
	if len(spec.Servers) > 0 {
		serverURL = spec.Servers[0].URL
	}

	var ops []Operation
	for path, item := range spec.Paths {
		for method, op := range item.methods() {
			if op.Deprecated {
				continue
			}
			ops = append(ops, convertOperation(path, method, op, spec.Components.Parameters))
		}
	}

	return &ParsedSpec{Operations: ops, ServerURL: serverURL}, nil
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
	defer func() { _ = resp.Body.Close() }()

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
	Servers []struct {
		URL string `yaml:"url"`
	} `yaml:"servers"`
	Paths      map[string]pathItem `yaml:"paths"`
	Components struct {
		Parameters map[string]yamlParameter `yaml:"parameters"`
	} `yaml:"components"`
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
	Ref         string     `yaml:"$ref"`
	Name        string     `yaml:"name"`
	In          string     `yaml:"in"`
	Required    bool       `yaml:"required"`
	Schema      yamlSchema `yaml:"schema"`
	Description string     `yaml:"description"`
}

type yamlSchema struct {
	Type  string     `yaml:"type"`
	Ref   string     `yaml:"$ref"`
	Enum  []string   `yaml:"enum"`
	Items *yamlSchema `yaml:"items"`
}

type yamlRequestBody struct {
	Content map[string]yamlMediaType `yaml:"content"`
}

type yamlMediaType struct {
	Schema yamlSchema `yaml:"schema"`
}

func convertOperation(path, method string, op *yamlOperation, componentParams map[string]yamlParameter) Operation {
	result := Operation{
		OperationID: op.OperationID,
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
	}

	for _, p := range op.Parameters {
		// Resolve $ref parameters from components
		if p.Ref != "" {
			refName := strings.TrimPrefix(p.Ref, "#/components/parameters/")
			if resolved, ok := componentParams[refName]; ok {
				p = resolved
			} else {
				continue
			}
		}
		if p.Name == "" {
			continue
		}
		paramType := p.Schema.Type
		hasEnum := len(p.Schema.Enum) > 0
		// Detect array params (schema has items but no explicit type: array)
		if p.Schema.Items != nil {
			paramType = "array"
			if len(p.Schema.Items.Enum) > 0 {
				hasEnum = true
			}
		}
		result.Parameters = append(result.Parameters, Parameter{
			Name:        p.Name,
			In:          p.In,
			Type:        paramType,
			Required:    p.Required,
			Description: p.Description,
			HasEnum:     hasEnum,
		})
	}

	if op.RequestBody != nil {
		result.RequestBodyRef = "true" // signal that body exists
		for ct, mt := range op.RequestBody.Content {
			result.BodyContentType = ct
			if mt.Schema.Ref != "" {
				result.RequestBodyRef = mt.Schema.Ref
			}
			break
		}
	}

	for _, sec := range op.Security {
		for _, scopes := range sec {
			result.Scopes = append(result.Scopes, scopes...)
		}
	}

	return result
}
