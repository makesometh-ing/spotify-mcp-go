package codegen

import "sort"

// ExtractScopes returns the deduplicated, alphabetically sorted union of all
// OAuth scopes required across all operations in the parsed spec.
func ExtractScopes(spec *ParsedSpec) []string {
	seen := make(map[string]bool)
	for _, op := range spec.Operations {
		for _, s := range op.Scopes {
			seen[s] = true
		}
	}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}
