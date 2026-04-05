package codegen

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

// OperationMeta holds spec-derived metadata for one API operation.
type OperationMeta struct {
	OperationID     string
	Method          string // "GET", "POST", etc.
	Path            string // "/playlists/{playlist_id}"
	Summary         string
	Description     string
	Scopes          []string
	ParamDescs      map[string]string // wire name -> description (path + query params)
	BodyDescs       map[string]string // wire name -> description (body properties)
	BodyContentType string            // e.g., "application/json", "image/jpeg"
}

// MetadataResult holds all extracted metadata, keyed by PascalCase method name.
type MetadataResult struct {
	Operations map[string]*OperationMeta // PascalCase name -> metadata
	ServerURL  string
}

// ExtractMetadata loads an OpenAPI spec and extracts metadata for all
// non-deprecated operations. Results are keyed by PascalCase method name
// (matching oapi-codegen's naming convention).
func ExtractMetadata(specData []byte) (*MetadataResult, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		return nil, fmt.Errorf("loading OpenAPI spec: %w", err)
	}

	result := &MetadataResult{
		Operations: make(map[string]*OperationMeta),
	}

	if len(doc.Servers) > 0 {
		result.ServerURL = doc.Servers[0].URL
	}

	if doc.Paths == nil {
		return result, nil
	}

	for path, item := range doc.Paths.Map() {
		for method, op := range pathItemMethods(item) {
			if op.Deprecated {
				continue
			}
			pascalName := kebabToCamel(op.OperationID)
			meta := &OperationMeta{
				OperationID:     op.OperationID,
				Method:          method,
				Path:            path,
				Summary:         op.Summary,
				Description:     op.Description,
				ParamDescs:      make(map[string]string),
				BodyDescs:       make(map[string]string),
			}

			// Extract scopes from security requirements
			if op.Security != nil {
				for _, sec := range *op.Security {
					for _, scopes := range sec {
						meta.Scopes = append(meta.Scopes, scopes...)
					}
				}
			}

			// Extract parameter descriptions
			for _, paramRef := range op.Parameters {
				p := paramRef.Value
				if p == nil {
					continue
				}
				if p.Description != "" {
					meta.ParamDescs[p.Name] = p.Description
				}
			}

			// Extract body field descriptions
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				for ct, mediaType := range op.RequestBody.Value.Content {
					meta.BodyContentType = ct
					if mediaType.Schema != nil && mediaType.Schema.Value != nil {
						for propName, propRef := range mediaType.Schema.Value.Properties {
							if propRef.Value != nil && propRef.Value.Description != "" {
								meta.BodyDescs[propName] = propRef.Value.Description
							}
						}
					}
					break
				}
			}

			result.Operations[pascalName] = meta
		}
	}

	return result, nil
}

func pathItemMethods(item *openapi3.PathItem) map[string]*openapi3.Operation {
	m := make(map[string]*openapi3.Operation)
	if item.Get != nil {
		m["GET"] = item.Get
	}
	if item.Put != nil {
		m["PUT"] = item.Put
	}
	if item.Post != nil {
		m["POST"] = item.Post
	}
	if item.Delete != nil {
		m["DELETE"] = item.Delete
	}
	if item.Patch != nil {
		m["PATCH"] = item.Patch
	}
	return m
}
