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

// flexRequired handles the OpenAPI `required` field which can be either
// a []string (schema-level list of required property names) or a bool
// (parameter-level required flag). Some specs mix these incorrectly.
type flexRequired []string

func (f *flexRequired) UnmarshalYAML(value *yaml.Node) error {
	// Try array of strings first (standard schema-level required)
	var arr []string
	if err := value.Decode(&arr); err == nil {
		*f = arr
		return nil
	}
	// If it's a bool or anything else, ignore it (not a property name list)
	*f = nil
	return nil
}

// Parameter represents an OpenAPI operation parameter.
type Parameter struct {
	Name        string
	In          string // "path" or "query"
	Type        string
	Required    bool
	Description string
	HasEnum     bool
}

// BodyField represents a property of a JSON request body schema.
type BodyField struct {
	Name        string
	Type        string // "string", "integer", "boolean", "array", "object"
	Required    bool
	Description string
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
	BodyFields      []BodyField
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
			ops = append(ops, convertOperation(path, method, op, spec.Components.Parameters, spec.Components.Schemas))
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
		Schemas    map[string]yamlSchemaObj `yaml:"schemas"`
	} `yaml:"components"`
}

// yamlSchemaObj represents a full schema object with properties and required list.
type yamlSchemaObj struct {
	Ref        string                    `yaml:"$ref"`
	Type       string                    `yaml:"type"`
	Required   flexRequired              `yaml:"required"`
	Properties map[string]yamlSchemaProp `yaml:"properties"`
}

// yamlSchemaProp represents a single property within a schema.
type yamlSchemaProp struct {
	Type        string      `yaml:"type"`
	Description string      `yaml:"description"`
	Items       *yamlSchema `yaml:"items"`
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
	Schema yamlSchemaObj `yaml:"schema"`
}

func convertOperation(path, method string, op *yamlOperation, componentParams map[string]yamlParameter, componentSchemas map[string]yamlSchemaObj) Operation {
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

			// Resolve schema: either from $ref or inline
			var schema *yamlSchemaObj
			if mt.Schema.Ref != "" {
				result.RequestBodyRef = mt.Schema.Ref
				schemaName := strings.TrimPrefix(mt.Schema.Ref, "#/components/schemas/")
				if resolved, ok := componentSchemas[schemaName]; ok {
					schema = &resolved
				}
			} else if len(mt.Schema.Properties) > 0 {
				schema = &mt.Schema
			}

			if schema != nil {
				requiredSet := make(map[string]bool, len(schema.Required))
				for _, r := range schema.Required {
					requiredSet[r] = true
				}
				for propName, prop := range schema.Properties {
					propType := prop.Type
					if prop.Items != nil && propType == "" {
						propType = "array"
					}
					result.BodyFields = append(result.BodyFields, BodyField{
						Name:        propName,
						Type:        propType,
						Required:    requiredSet[propName],
						Description: prop.Description,
					})
				}
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
