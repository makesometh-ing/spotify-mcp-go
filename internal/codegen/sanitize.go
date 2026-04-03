package codegen

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SanitizeSpec fixes known quirks in real-world OpenAPI specs that cause
// strict parsers to fail. Spotify's spec uses `required: true` at the
// individual property level (Swagger 2.0 style), which is invalid in
// OpenAPI 3.0 where `required` must be an array of property names at
// the schema level.
//
// This function converts the invalid format to the valid one: for each
// schema with `properties`, it collects property-level `required: true`
// fields, removes them, and adds the property names to the parent
// schema's `required` array.
func SanitizeSpec(data []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing YAML for sanitization: %w", err)
	}

	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		convertPropertyRequired(root.Content[0])
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("re-serializing sanitized YAML: %w", err)
	}
	return out, nil
}

// convertPropertyRequired walks the entire YAML tree. At every mapping node
// that has a "properties" key (i.e., an OpenAPI schema object), it scans
// each property definition for `required: true/false`. For each property
// with `required: true`, it removes the boolean field and adds the property
// name to the parent schema's `required` array. Properties with
// `required: false` have the field removed (it's the default).
func convertPropertyRequired(node *yaml.Node) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.MappingNode:
		// If this mapping has "properties", fix required at property level
		propsIdx := findKey(node, "properties")
		if propsIdx >= 0 {
			fixSchemaRequired(node, propsIdx)
		}
		// Also strip boolean required from leaf schemas (type but no properties).
		// In OpenAPI 3.0, a non-object schema (e.g., type: string) cannot have
		// required as a boolean. This catches cases like Spotify's image/jpeg
		// body schema: { type: string, required: true }.
		stripLeafSchemaRequired(node)
		// Recurse into all values
		for i := 1; i < len(node.Content); i += 2 {
			convertPropertyRequired(node.Content[i])
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			convertPropertyRequired(child)
		}
	}
}

// fixSchemaRequired processes a schema mapping node that has "properties".
// It extracts boolean `required` from each property and builds the schema-level
// `required` array.
func fixSchemaRequired(schema *yaml.Node, propsIdx int) {
	propsNode := schema.Content[propsIdx+1]
	if propsNode.Kind != yaml.MappingNode {
		return
	}

	var requiredNames []string

	for i := 0; i < len(propsNode.Content)-1; i += 2 {
		propName := propsNode.Content[i]
		propDef := propsNode.Content[i+1]
		if propDef.Kind != yaml.MappingNode {
			continue
		}

		reqIdx := findKey(propDef, "required")
		if reqIdx < 0 {
			continue
		}
		reqVal := propDef.Content[reqIdx+1]
		if reqVal.Kind != yaml.ScalarNode {
			continue
		}
		if reqVal.Value != "true" && reqVal.Value != "false" {
			continue
		}

		if reqVal.Value == "true" {
			requiredNames = append(requiredNames, propName.Value)
		}
		// Remove the boolean required from this property definition
		propDef.Content = append(propDef.Content[:reqIdx], propDef.Content[reqIdx+2:]...)
		// Adjust index since we removed two nodes
		i -= 0 // i is for the outer loop over properties, not affected
	}

	if len(requiredNames) == 0 {
		return
	}

	// Merge into existing required array or create a new one
	existingIdx := findKey(schema, "required")
	if existingIdx >= 0 {
		reqArray := schema.Content[existingIdx+1]
		if reqArray.Kind == yaml.SequenceNode {
			for _, name := range requiredNames {
				if !sequenceContains(reqArray, name) {
					reqArray.Content = append(reqArray.Content, &yaml.Node{
						Kind:  yaml.ScalarNode,
						Value: name,
					})
				}
			}
		}
	} else {
		items := make([]*yaml.Node, len(requiredNames))
		for i, name := range requiredNames {
			items[i] = &yaml.Node{Kind: yaml.ScalarNode, Value: name}
		}
		schema.Content = append(schema.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "required"},
			&yaml.Node{Kind: yaml.SequenceNode, Content: items},
		)
	}
}

// stripLeafSchemaRequired removes `required: true/false` from schema mappings
// that have a `type` key but no `properties` key. These are leaf schemas (string,
// number, array, etc.) where `required` as a boolean is invalid in OpenAPI 3.0.
// Parameter objects (which have `name` and `in` keys) are left alone since
// `required: true` is valid for parameters.
func stripLeafSchemaRequired(node *yaml.Node) {
	reqIdx := findKey(node, "required")
	if reqIdx < 0 {
		return
	}
	reqVal := node.Content[reqIdx+1]
	if reqVal.Kind != yaml.ScalarNode || (reqVal.Value != "true" && reqVal.Value != "false") {
		return
	}
	// Skip parameter objects (they legitimately have required: bool)
	if findKey(node, "in") >= 0 && findKey(node, "name") >= 0 {
		return
	}
	// This is a schema with a boolean required, remove it
	if findKey(node, "type") >= 0 || findKey(node, "schema") >= 0 {
		node.Content = append(node.Content[:reqIdx], node.Content[reqIdx+2:]...)
	}
}

func findKey(mapping *yaml.Node, key string) int {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func sequenceContains(seq *yaml.Node, val string) bool {
	for _, n := range seq.Content {
		if n.Value == val {
			return true
		}
	}
	return false
}
