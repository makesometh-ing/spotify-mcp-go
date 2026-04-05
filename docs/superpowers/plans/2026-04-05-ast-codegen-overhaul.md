# AST-Based Codegen Overhaul Implementation Plan [SPO-46]

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-rolled OpenAPI YAML parser with a two-phase pipeline: `go/ast` inspection of oapi-codegen output + kin-openapi metadata extraction, generating per-endpoint tool files with typed struct handlers instead of `map[string]interface{}` round-trips.

**Architecture:** Phase 1 (AST inspector) parses `generated_client.go` and `generated_types.go` to derive method signatures, parameter types, and required/optional status. Phase 2 (metadata extractor) uses kin-openapi to get descriptions, scopes, and operation ID mapping. A merge function joins them by PascalCase method name. The code generator produces one file per endpoint plus an aggregator.

**Tech Stack:** Go stdlib `go/ast`/`go/parser`/`go/token`, `github.com/getkin/kin-openapi/openapi3` (already a dependency), `github.com/mark3labs/mcp-go`, `gopkg.in/h2non/gock.v1` (new test dependency)

---

## File Structure

### New files
| File | Responsibility |
|------|---------------|
| `internal/codegen/inspector.go` | AST inspection of generated client/types files |
| `internal/codegen/inspector_test.go` | Tests for inspector |
| `internal/codegen/metadata.go` | kin-openapi metadata extraction (descriptions, scopes) |
| `internal/codegen/metadata_test.go` | Tests for metadata extractor |
| `internal/codegen/testdata/fixture_client.go` | Minimal oapi-codegen-style client fixture for inspector tests |
| `internal/codegen/testdata/fixture_types.go` | Minimal oapi-codegen-style types fixture for inspector tests |
| `internal/tools/generated_tool_*.go` | One per endpoint (generated output, ~48 files) |
| `internal/tools/generated_tools_all.go` | Aggregator with AllTools, AllRegistrations, AllScopes, helpers |
| `internal/tools/generated_tools_integration_test.go` | gock-based integration test |

### Modified files
| File | Change |
|------|--------|
| `internal/codegen/tools_gen.go` | Complete rewrite: new templates, per-file generation, merge logic |
| `internal/codegen/tools_gen_test.go` | Complete rewrite to test new generation |
| `cmd/codegen/main.go` | Updated pipeline: AST inspect + metadata extract + per-file generate |
| `go.mod` / `go.sum` | Add `gopkg.in/h2non/gock.v1` |

### Deleted files
| File | Reason |
|------|--------|
| `internal/codegen/parser.go` | Replaced by inspector.go + metadata.go |
| `internal/codegen/parser_test.go` | Tests for deleted code |
| `internal/codegen/sanitize.go` | kin-openapi handles spec quirks natively |
| `internal/codegen/scopes.go` | Absorbed into metadata.go |
| `internal/codegen/scopes_test.go` | Tests for deleted code |
| `internal/codegen/generate_integration_test.go` | Depends on deleted Parse/SanitizeSpec pipeline |
| `internal/tools/generated_tools.go` | Replaced by per-endpoint files + aggregator |

---

## Task 1: AST Inspector

**Files:**
- Create: `internal/codegen/testdata/fixture_client.go`
- Create: `internal/codegen/testdata/fixture_types.go`
- Create: `internal/codegen/inspector.go`
- Create: `internal/codegen/inspector_test.go`

### Overview

The inspector parses Go source files produced by oapi-codegen. It extracts:
- Method signatures from `ClientWithResponses` (path params, query param struct, body type)
- Struct field details from `*Params` and `*JSONBody` structs (wire names from tags, pointer/required, Go types)
- Type aliases (`type FooJSONRequestBody = FooJSONBody`)

### Type Definitions

These types go in `inspector.go`:

```go
// MethodInfo represents a parsed ClientWithResponses method signature.
type MethodInfo struct {
    Name           string      // e.g., "AddItemsToPlaylist" (without "WithResponse" suffix)
    PathParams     []PathParam // positional params that aren't context, params struct, body, or variadic
    ParamsTypeName string      // e.g., "AddItemsToPlaylistParams" or "" if none
    BodyTypeName   string      // e.g., "AddItemsToPlaylistJSONRequestBody" or "" if none
    IsNonJSONBody  bool        // true if this is a WithBodyWithResponse method (io.Reader body)
    BodyContentType string    // for non-JSON bodies, the content type param name (always "contentType")
}

// PathParam is a positional parameter in the method signature.
type PathParam struct {
    GoName string // Go parameter name (e.g., "playlistId", "id", "pType")
    GoType string // Go type expression (e.g., "PathPlaylistId", "string")
}

// FieldInfo represents a struct field with tag metadata.
type FieldInfo struct {
    GoName   string // Go field name (e.g., "Market", "Uris")
    WireName string // wire name from form:/json: tag (e.g., "market", "uris")
    GoType   string // full Go type expression (e.g., "*string", "[]string", "*int")
    Required bool   // true if non-pointer type
    MCPType  string // "String", "Number", "Boolean", "Array"
}

// InspectResult holds everything extracted from the generated Go files.
type InspectResult struct {
    Methods     []*MethodInfo            // all ClientWithResponses methods (filtered)
    Structs     map[string][]FieldInfo   // struct name -> fields
    TypeAliases map[string]string        // alias -> target (e.g., "FooJSONRequestBody" -> "FooJSONBody")
}
```

### Fixture Files

- [ ] **Step 1: Create test fixture `fixture_client.go`**

Write `internal/codegen/testdata/fixture_client.go`. This is a minimal Go file mimicking oapi-codegen output, covering all method signature patterns:

```go
package spotify

import (
	"context"
	"io"
	"net/http"
)

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type PathPlaylistId = string
type PathArtistId = string

type ClientWithResponses struct{}

// Pattern: no params, no body
func (c *ClientWithResponses) GetCurrentUsersProfileWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path param only
func (c *ClientWithResponses) GetAnArtistWithResponse(ctx context.Context, id PathArtistId, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + query params
func (c *ClientWithResponses) GetPlaylistWithResponse(ctx context.Context, playlistId PathPlaylistId, params *GetPlaylistParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: query params only
func (c *ClientWithResponses) SearchWithResponse(ctx context.Context, params *SearchParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: body only (JSON)
func (c *ClientWithResponses) CreatePlaylistWithResponse(ctx context.Context, body CreatePlaylistJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + query + body (JSON)
func (c *ClientWithResponses) AddItemsToPlaylistWithResponse(ctx context.Context, playlistId PathPlaylistId, params *AddItemsToPlaylistParams, body AddItemsToPlaylistJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + body (JSON), with WithBody variant
func (c *ClientWithResponses) ChangePlaylistDetailsWithBodyWithResponse(ctx context.Context, playlistId PathPlaylistId, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}
func (c *ClientWithResponses) ChangePlaylistDetailsWithResponse(ctx context.Context, playlistId PathPlaylistId, body ChangePlaylistDetailsJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: non-JSON body only (no JSON variant exists)
func (c *ClientWithResponses) UploadCustomPlaylistCoverWithBodyWithResponse(ctx context.Context, playlistId PathPlaylistId, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path param with enum type (not a Path* alias)
func (c *ClientWithResponses) GetUsersTopItemsWithResponse(ctx context.Context, pType GetUsersTopItemsParamsType, params *GetUsersTopItemsParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}
```

- [ ] **Step 2: Create test fixture `fixture_types.go`**

Write `internal/codegen/testdata/fixture_types.go`:

```go
package spotify

type QueryMarket = string

type GetPlaylistParams struct {
	Market *QueryMarket `form:"market,omitempty" json:"market,omitempty"`
	Fields *string      `form:"fields,omitempty" json:"fields,omitempty"`
}

type SearchParams struct {
	Q    string                  `form:"q" json:"q"`
	Type []SearchParamsType      `form:"type" json:"type"`
}

type SearchParamsType string

type AddItemsToPlaylistParams struct {
	Position *int    `form:"position,omitempty" json:"position,omitempty"`
	Uris     *string `form:"uris,omitempty" json:"uris,omitempty"`
}

type GetUsersTopItemsParams struct {
	TimeRange *string `form:"time_range,omitempty" json:"time_range,omitempty"`
	Limit     *int    `form:"limit,omitempty" json:"limit,omitempty"`
}

type GetUsersTopItemsParamsType string

type CreatePlaylistJSONRequestBody = CreatePlaylistJSONBody
type CreatePlaylistJSONBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Public      *bool   `json:"public,omitempty"`
}

type AddItemsToPlaylistJSONRequestBody = AddItemsToPlaylistJSONBody
type AddItemsToPlaylistJSONBody struct {
	Position *int      `json:"position,omitempty"`
	Uris     *[]string `json:"uris,omitempty"`
}

type ChangePlaylistDetailsJSONRequestBody = ChangePlaylistDetailsJSONBody
type ChangePlaylistDetailsJSONBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Public      *bool   `json:"public,omitempty"`
}
```

- [ ] **Step 3: Write failing test for `Inspect`**

Write `internal/codegen/inspector_test.go`:

```go
package codegen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

func TestInspect(t *testing.T) {
	clientSrc := loadFixture(t, "fixture_client.go")
	typesSrc := loadFixture(t, "fixture_types.go")

	result, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	// Should find 8 methods (ChangePlaylistDetailsWithBody is skipped because JSON variant exists)
	require.Len(t, result.Methods, 8)

	byName := make(map[string]*MethodInfo)
	for _, m := range result.Methods {
		byName[m.Name] = m
	}

	t.Run("no params no body", func(t *testing.T) {
		m := byName["GetCurrentUsersProfile"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Empty(t, m.ParamsTypeName)
		assert.Empty(t, m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
	})

	t.Run("path param only", func(t *testing.T) {
		m := byName["GetAnArtist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "id", m.PathParams[0].GoName)
		assert.Equal(t, "PathArtistId", m.PathParams[0].GoType)
		assert.Empty(t, m.ParamsTypeName)
	})

	t.Run("path plus query", func(t *testing.T) {
		m := byName["GetPlaylist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "playlistId", m.PathParams[0].GoName)
		assert.Equal(t, "GetPlaylistParams", m.ParamsTypeName)
	})

	t.Run("query only", func(t *testing.T) {
		m := byName["Search"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Equal(t, "SearchParams", m.ParamsTypeName)
	})

	t.Run("body only JSON", func(t *testing.T) {
		m := byName["CreatePlaylist"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Equal(t, "CreatePlaylistJSONRequestBody", m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
	})

	t.Run("path plus query plus body", func(t *testing.T) {
		m := byName["AddItemsToPlaylist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "AddItemsToPlaylistParams", m.ParamsTypeName)
		assert.Equal(t, "AddItemsToPlaylistJSONRequestBody", m.BodyTypeName)
	})

	t.Run("JSON body preferred over WithBody variant", func(t *testing.T) {
		m := byName["ChangePlaylistDetails"]
		require.NotNil(t, m)
		assert.Equal(t, "ChangePlaylistDetailsJSONRequestBody", m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
		// WithBody variant should not be a separate entry
		assert.Nil(t, byName["ChangePlaylistDetailsWithBody"])
	})

	t.Run("non-JSON body", func(t *testing.T) {
		m := byName["UploadCustomPlaylistCover"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.True(t, m.IsNonJSONBody)
		assert.Empty(t, m.BodyTypeName)
	})

	t.Run("enum path param", func(t *testing.T) {
		m := byName["GetUsersTopItems"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "pType", m.PathParams[0].GoName)
		assert.Equal(t, "GetUsersTopItemsParamsType", m.PathParams[0].GoType)
		assert.Equal(t, "GetUsersTopItemsParams", m.ParamsTypeName)
	})
}

func TestInspectStructFields(t *testing.T) {
	clientSrc := loadFixture(t, "fixture_client.go")
	typesSrc := loadFixture(t, "fixture_types.go")

	result, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	t.Run("query params struct", func(t *testing.T) {
		fields, ok := result.Structs["GetPlaylistParams"]
		require.True(t, ok)
		require.Len(t, fields, 2)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		market := byWire["market"]
		assert.Equal(t, "Market", market.GoName)
		assert.False(t, market.Required)
		assert.Equal(t, "String", market.MCPType)

		fields2 := byWire["fields"]
		assert.False(t, fields2.Required)
		assert.Equal(t, "String", fields2.MCPType)
	})

	t.Run("required query params", func(t *testing.T) {
		fields, ok := result.Structs["SearchParams"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		q := byWire["q"]
		assert.True(t, q.Required)
		assert.Equal(t, "String", q.MCPType)

		tp := byWire["type"]
		assert.True(t, tp.Required)
		assert.Equal(t, "Array", tp.MCPType)
	})

	t.Run("body struct via alias resolution", func(t *testing.T) {
		fields, ok := result.Structs["CreatePlaylistJSONBody"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		name := byWire["name"]
		assert.True(t, name.Required)
		assert.Equal(t, "String", name.MCPType)

		desc := byWire["description"]
		assert.False(t, desc.Required)

		pub := byWire["public"]
		assert.False(t, pub.Required)
		assert.Equal(t, "Boolean", pub.MCPType)
	})

	t.Run("body with array field", func(t *testing.T) {
		fields, ok := result.Structs["AddItemsToPlaylistJSONBody"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		uris := byWire["uris"]
		assert.Equal(t, "Array", uris.MCPType)
		assert.False(t, uris.Required) // *[]string is optional
	})

	t.Run("type alias resolution", func(t *testing.T) {
		target, ok := result.TypeAliases["CreatePlaylistJSONRequestBody"]
		require.True(t, ok)
		assert.Equal(t, "CreatePlaylistJSONBody", target)
	})
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestInspect -v`

Expected: FAIL with "undefined: Inspect"

- [ ] **Step 5: Implement `Inspect` function**

Write `internal/codegen/inspector.go`:

```go
package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// [Type definitions from above: MethodInfo, PathParam, FieldInfo, InspectResult]

// Inspect parses oapi-codegen output files and extracts method signatures,
// struct field metadata, and type aliases.
func Inspect(clientSrc, typesSrc []byte) (*InspectResult, error) {
	fset := token.NewFileSet()

	clientFile, err := parser.ParseFile(fset, "client.go", clientSrc, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing client source: %w", err)
	}

	typesFile, err := parser.ParseFile(fset, "types.go", typesSrc, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing types source: %w", err)
	}

	result := &InspectResult{
		Structs:     make(map[string][]FieldInfo),
		TypeAliases: make(map[string]string),
	}

	// Pass 1: collect structs and type aliases from types file
	for _, decl := range typesFile.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Assign != 0 {
				// Type alias: type Foo = Bar
				result.TypeAliases[ts.Name.Name] = typeExprString(ts.Type)
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			result.Structs[ts.Name.Name] = extractFields(st)
		}
	}

	// Pass 2: collect ClientWithResponses methods from client file
	// First, find all method names to detect WithBody/JSON pairs
	allMethodNames := map[string]bool{}
	for _, decl := range clientFile.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil {
			continue
		}
		if !isClientWithResponses(fd.Recv) {
			continue
		}
		if strings.HasSuffix(fd.Name.Name, "WithResponse") {
			allMethodNames[fd.Name.Name] = true
		}
	}

	for _, decl := range clientFile.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil {
			continue
		}
		if !isClientWithResponses(fd.Recv) {
			continue
		}
		name := fd.Name.Name
		if !strings.HasSuffix(name, "WithResponse") {
			continue
		}

		isWithBody := strings.HasSuffix(name, "WithBodyWithResponse")
		if isWithBody {
			// Skip if a JSON variant exists
			jsonVariant := strings.TrimSuffix(name, "WithBodyWithResponse") + "WithResponse"
			if allMethodNames[jsonVariant] {
				continue
			}
		}

		mi := parseMethod(fd, isWithBody)
		result.Methods = append(result.Methods, mi)
	}

	// Also collect structs from client file (some may be defined there)
	for _, decl := range clientFile.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Assign != 0 {
				if _, exists := result.TypeAliases[ts.Name.Name]; !exists {
					result.TypeAliases[ts.Name.Name] = typeExprString(ts.Type)
				}
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if _, exists := result.Structs[ts.Name.Name]; !exists {
				result.Structs[ts.Name.Name] = extractFields(st)
			}
		}
	}

	return result, nil
}

func parseMethod(fd *ast.FuncDecl, isNonJSON bool) *MethodInfo {
	name := fd.Name.Name
	if isNonJSON {
		name = strings.TrimSuffix(name, "WithBodyWithResponse")
	} else {
		name = strings.TrimSuffix(name, "WithResponse")
	}

	mi := &MethodInfo{
		Name:          name,
		IsNonJSONBody: isNonJSON,
	}

	params := fd.Type.Params.List
	for _, field := range params {
		typeStr := typeExprString(field.Type)

		// Skip context.Context
		if typeStr == "context.Context" {
			continue
		}
		// Skip ...RequestEditorFn (variadic)
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			continue
		}
		// Skip io.Reader (non-JSON body content)
		if typeStr == "io.Reader" {
			continue
		}
		// Skip contentType string for WithBody methods
		if isNonJSON && typeStr == "string" && len(field.Names) > 0 && field.Names[0].Name == "contentType" {
			continue
		}

		paramName := ""
		if len(field.Names) > 0 {
			paramName = field.Names[0].Name
		}

		// Check if it's a *Params pointer type
		if star, ok := field.Type.(*ast.StarExpr); ok {
			innerType := typeExprString(star.X)
			if strings.HasSuffix(innerType, "Params") {
				mi.ParamsTypeName = innerType
				continue
			}
		}

		// Check if it matches JSONRequestBody
		if strings.HasSuffix(typeStr, "JSONRequestBody") {
			mi.BodyTypeName = typeStr
			continue
		}

		// Everything else is a path param
		mi.PathParams = append(mi.PathParams, PathParam{
			GoName: paramName,
			GoType: typeStr,
		})
	}

	return mi
}

func isClientWithResponses(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "ClientWithResponses"
}

func extractFields(st *ast.StructType) []FieldInfo {
	var fields []FieldInfo
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue // embedded field
		}
		// Skip AdditionalProperties
		if f.Names[0].Name == "AdditionalProperties" {
			continue
		}

		fi := FieldInfo{
			GoName: f.Names[0].Name,
			GoType: typeExprString(f.Type),
		}

		// Determine wire name from struct tag
		if f.Tag != nil {
			tag := strings.Trim(f.Tag.Value, "`")
			fi.WireName = wireNameFromTag(tag)
		}

		// Determine required: non-pointer, non-slice = required
		// But *[]T and []T are both arrays
		fi.Required = !isPointerType(f.Type)
		fi.MCPType = goTypeToMCPType(f.Type)

		fields = append(fields, fi)
	}
	return fields
}

func wireNameFromTag(tag string) string {
	// Try form: tag first (query params), then json: tag (body fields)
	st := reflect.StructTag(tag)
	for _, key := range []string{"form", "json"} {
		val, ok := st.Lookup(key)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(val, ",")
		if name != "" && name != "-" {
			return name
		}
	}
	return ""
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

func goTypeToMCPType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return goTypeToMCPType(e.X) // unwrap pointer
	case *ast.ArrayType:
		return "Array"
	case *ast.Ident:
		switch e.Name {
		case "int", "int32", "int64", "float32", "float64":
			return "Number"
		case "bool":
			return "Boolean"
		default:
			return "String" // string, named string types
		}
	case *ast.SelectorExpr:
		return "String" // qualified types like time.Time
	case *ast.MapType:
		return "Object"
	default:
		return "String"
	}
}

func typeExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + typeExprString(e.X)
	case *ast.ArrayType:
		return "[]" + typeExprString(e.Elt)
	case *ast.SelectorExpr:
		return typeExprString(e.X) + "." + e.Sel.Name
	case *ast.Ellipsis:
		return "..." + typeExprString(e.Elt)
	case *ast.MapType:
		return "map[" + typeExprString(e.Key) + "]" + typeExprString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "unknown"
	}
}

// ResolveBodyStruct resolves a JSONRequestBody type name to its underlying
// struct fields. It follows type aliases (e.g., FooJSONRequestBody = FooJSONBody)
// and looks up the target struct.
func (r *InspectResult) ResolveBodyStruct(typeName string) ([]FieldInfo, bool) {
	// Direct struct lookup
	if fields, ok := r.Structs[typeName]; ok {
		return fields, true
	}
	// Follow alias
	if target, ok := r.TypeAliases[typeName]; ok {
		if fields, ok := r.Structs[target]; ok {
			return fields, true
		}
	}
	return nil, false
}
```

Note: add `"fmt"` to the import list.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestInspect -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/codegen/inspector.go internal/codegen/inspector_test.go internal/codegen/testdata/fixture_client.go internal/codegen/testdata/fixture_types.go
git commit -m "feat(codegen): add AST inspector for generated client types [SPO-46]"
```

---

## Task 2: Metadata Extractor

**Files:**
- Create: `internal/codegen/metadata.go`
- Create: `internal/codegen/metadata_test.go`

### Overview

Uses kin-openapi to load the OpenAPI spec and extract metadata that the AST can't provide: operation IDs, HTTP methods/paths, descriptions, scopes, and parameter descriptions. Produces a map keyed by PascalCase method name for joining with AST data.

### Type Definitions

```go
// OperationMeta holds spec-derived metadata for one API operation.
type OperationMeta struct {
    OperationID string
    Method      string // "GET", "POST", etc.
    Path        string // "/playlists/{playlist_id}"
    Summary     string
    Description string
    Scopes      []string
    ParamDescs  map[string]string // wire name -> description (path + query params)
    BodyDescs   map[string]string // wire name -> description (body properties)
    BodyContentType string        // e.g., "application/json", "image/jpeg"
}

// MetadataResult holds all extracted metadata, keyed by PascalCase method name.
type MetadataResult struct {
    Operations map[string]*OperationMeta // PascalCase name -> metadata
    ServerURL  string
}
```

- [ ] **Step 1: Write failing test for `ExtractMetadata`**

Write `internal/codegen/metadata_test.go`:

```go
package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMetadata(t *testing.T) {
	specData := loadFixture(t, "spotify_fixture.yaml")

	result, err := ExtractMetadata(specData)
	require.NoError(t, err)

	assert.Equal(t, "https://api.spotify.com/v1", result.ServerURL)

	// 4 active operations (transfer-playback is deprecated)
	require.Len(t, result.Operations, 4)

	t.Run("get-playlist metadata", func(t *testing.T) {
		op := result.Operations["GetPlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "get-playlist", op.OperationID)
		assert.Equal(t, "GET", op.Method)
		assert.Equal(t, "/playlists/{playlist_id}", op.Path)
		assert.Equal(t, "Get Playlist", op.Summary)
		assert.Contains(t, op.Description, "Get a playlist owned by")
		assert.Equal(t, []string{"playlist-read-private"}, op.Scopes)
		assert.Equal(t, "The Spotify ID of the playlist.", op.ParamDescs["playlist_id"])
		assert.Equal(t, "An ISO 3166-1 alpha-2 country code.", op.ParamDescs["market"])
	})

	t.Run("add-tracks-to-playlist metadata", func(t *testing.T) {
		op := result.Operations["AddTracksToPlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "add-tracks-to-playlist", op.OperationID)
		assert.Equal(t, "POST", op.Method)
		assert.Equal(t, "application/json", op.BodyContentType)
		assert.Equal(t, "Spotify track URIs to add.", op.BodyDescs["uris"])
		assert.Equal(t, "Position to insert items.", op.BodyDescs["position"])
	})

	t.Run("search metadata", func(t *testing.T) {
		op := result.Operations["Search"]
		require.NotNil(t, op)
		assert.Equal(t, "search", op.OperationID)
		assert.Equal(t, "GET", op.Method)
		assert.Empty(t, op.Scopes) // oauth_2_0: [] means no specific scopes
	})

	t.Run("create-playlist metadata", func(t *testing.T) {
		op := result.Operations["CreatePlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "create-playlist", op.OperationID)
		assert.Equal(t, "POST", op.Method)
		assert.Equal(t, "The name for the new playlist.", op.BodyDescs["name"])
	})

	t.Run("deprecated operation excluded", func(t *testing.T) {
		assert.Nil(t, result.Operations["TransferPlayback"])
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestExtractMetadata -v`

Expected: FAIL with "undefined: ExtractMetadata"

- [ ] **Step 3: Implement `ExtractMetadata`**

Write `internal/codegen/metadata.go`:

```go
package codegen

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// [Type definitions: OperationMeta, MetadataResult from above]

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
			for _, sec := range op.Security {
				for _, scopes := range *sec {
					meta.Scopes = append(meta.Scopes, scopes...)
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
						schema := mediaType.Schema.Value
						// Resolve $ref if needed (kin-openapi handles this)
						for propName, propRef := range schema.Properties {
							if propRef.Value != nil && propRef.Value.Description != "" {
								meta.BodyDescs[propName] = propRef.Value.Description
							}
						}
					}
					break // take first content type
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
```

Note: `kebabToCamel` is already defined in `tools_gen.go` and reused here.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestExtractMetadata -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/metadata.go internal/codegen/metadata_test.go
git commit -m "feat(codegen): add kin-openapi metadata extractor [SPO-46]"
```

---

## Task 3: Merge Logic and Unified ToolData

**Files:**
- Create: `internal/codegen/merge.go`
- Create: `internal/codegen/merge_test.go`

### Overview

Joins AST-derived method signatures with spec-derived metadata. Produces a unified `ToolData` list ready for template rendering. Handles name collision resolution (body wins over query when same wire name appears in both).

- [ ] **Step 1: Write failing test for `MergeToolData`**

Write `internal/codegen/merge_test.go`:

```go
package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeToolData(t *testing.T) {
	clientSrc := loadFixture(t, "fixture_client.go")
	typesSrc := loadFixture(t, "fixture_types.go")
	specData := loadFixture(t, "spotify_fixture.yaml")

	inspect, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	meta, err := ExtractMetadata(specData)
	require.NoError(t, err)

	tools := MergeToolData(inspect, meta)

	// Only operations present in BOTH AST and metadata should appear.
	// Fixture client has: GetCurrentUsersProfile, GetAnArtist, GetPlaylist,
	// Search, CreatePlaylist, AddItemsToPlaylist, ChangePlaylistDetails,
	// UploadCustomPlaylistCover, GetUsersTopItems.
	// Fixture spec has: GetPlaylist, AddTracksToPlaylist, GetPlaybackState,
	// CreatePlaylist, Search.
	// Intersection: GetPlaylist, CreatePlaylist, Search.
	// (AddTracksToPlaylist != AddItemsToPlaylist, different operationId)
	require.Len(t, tools, 3)

	byID := make(map[string]*ToolData)
	for i := range tools {
		byID[tools[i].OperationID] = &tools[i]
	}

	t.Run("get-playlist merged", func(t *testing.T) {
		td := byID["get-playlist"]
		require.NotNil(t, td)
		assert.Equal(t, "GetPlaylist", td.CamelName)
		assert.Equal(t, "Get Playlist", td.Summary)
		assert.Contains(t, td.Description, "Get a playlist owned by")
		assert.Equal(t, "GET", td.Method)
		assert.Equal(t, "/playlists/{playlist_id}", td.PathPattern)

		// Path params get wire names from metadata
		require.Len(t, td.PathParams, 1)
		assert.Equal(t, "playlist_id", td.PathParams[0].WireName)
		assert.Equal(t, "playlistId", td.PathParams[0].GoVarName)
		assert.Equal(t, "PathPlaylistId", td.PathParams[0].GoType)

		// Query params from struct
		require.Len(t, td.QueryParams, 2)
	})

	t.Run("search merged", func(t *testing.T) {
		td := byID["search"]
		require.NotNil(t, td)
		assert.Empty(t, td.PathParams)
		require.Len(t, td.QueryParams, 2)

		byWire := make(map[string]*ToolParamData)
		for i := range td.QueryParams {
			byWire[td.QueryParams[i].WireName] = &td.QueryParams[i]
		}

		q := byWire["q"]
		require.NotNil(t, q)
		assert.True(t, q.Required)
		assert.Equal(t, "String", q.MCPType)
		assert.Equal(t, "Search query keywords.", q.Description)

		tp := byWire["type"]
		require.NotNil(t, tp)
		assert.True(t, tp.Required)
		assert.Equal(t, "Array", tp.MCPType)
	})

	t.Run("create-playlist merged", func(t *testing.T) {
		td := byID["create-playlist"]
		require.NotNil(t, td)
		assert.True(t, td.HasJSONBody)
		require.Len(t, td.BodyParams, 3)

		byWire := make(map[string]*ToolParamData)
		for i := range td.BodyParams {
			byWire[td.BodyParams[i].WireName] = &td.BodyParams[i]
		}

		name := byWire["name"]
		require.NotNil(t, name)
		assert.True(t, name.Required)
		assert.Equal(t, "The name for the new playlist.", name.Description)
	})

	t.Run("sorted by operation ID", func(t *testing.T) {
		assert.True(t, tools[0].OperationID < tools[1].OperationID)
		assert.True(t, tools[1].OperationID < tools[2].OperationID)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestMergeToolData -v`

Expected: FAIL with "undefined: MergeToolData"

- [ ] **Step 3: Implement the merge types and function**

Write `internal/codegen/merge.go`:

```go
package codegen

import (
	"sort"
	"strings"
)

// ToolData is the unified data model for one tool, ready for template rendering.
type ToolData struct {
	OperationID string
	CamelName   string // PascalCase, e.g., "AddItemsToPlaylist"
	VarName     string // e.g., "AddItemsToPlaylistTool"
	HandlerName string // e.g., "NewAddItemsToPlaylistHandler"
	Summary     string
	Description string // full description for mcp.WithDescription
	Scopes      []string
	Method      string // HTTP method
	PathPattern string // URL path pattern

	HasBody     bool // true if any body
	HasJSONBody bool // true if JSON body (use JSONRequestBody type)
	IsNonJSONBody bool // true if non-JSON body (use WithBodyWithResponse)
	BodyContentType string // e.g., "image/jpeg" for non-JSON

	ParamsTypeName string // e.g., "AddItemsToPlaylistParams" or ""
	BodyTypeName   string // e.g., "AddItemsToPlaylistJSONRequestBody" or ""

	PathParams  []ToolPathParam
	QueryParams []ToolParamData
	BodyParams  []ToolParamData
	AllParams   []ToolParamData // union of query + body (deduplicated), for tool definition
}

// ToolPathParam represents a path parameter in the handler.
type ToolPathParam struct {
	WireName  string // e.g., "playlist_id"
	GoVarName string // e.g., "playlistId"
	GoType    string // e.g., "PathPlaylistId"
	Description string
}

// ToolParamData represents a query or body parameter.
type ToolParamData struct {
	WireName    string // e.g., "market", "uris"
	GoFieldName string // e.g., "Market", "Uris"
	GoType      string // e.g., "*string", "[]string"
	MCPType     string // "String", "Number", "Boolean", "Array"
	Required    bool
	Description string
	IsArray     bool   // convenience: MCPType == "Array"
}

// MergeToolData joins AST inspection results with metadata to produce
// a sorted list of ToolData ready for code generation. Only operations
// present in both the AST and metadata are included.
func MergeToolData(inspect *InspectResult, meta *MetadataResult) []ToolData {
	var tools []ToolData

	for _, method := range inspect.Methods {
		opMeta, ok := meta.Operations[method.Name]
		if !ok {
			continue // method exists in client but not in spec (shouldn't happen)
		}

		td := ToolData{
			OperationID:     opMeta.OperationID,
			CamelName:       method.Name,
			VarName:         method.Name + "Tool",
			HandlerName:     "New" + method.Name + "Handler",
			Summary:         opMeta.Summary,
			Description:     toolDescription(opMeta.Summary, opMeta.Description),
			Scopes:          opMeta.Scopes,
			Method:          opMeta.Method,
			PathPattern:     opMeta.Path,
			ParamsTypeName:  method.ParamsTypeName,
			BodyTypeName:    method.BodyTypeName,
			HasBody:         method.BodyTypeName != "" || method.IsNonJSONBody,
			HasJSONBody:     method.BodyTypeName != "",
			IsNonJSONBody:   method.IsNonJSONBody,
			BodyContentType: opMeta.BodyContentType,
		}

		// Resolve path params: match AST positional params with spec param names
		specPathParams := specPathParamNames(opMeta)
		for i, pp := range method.PathParams {
			wireName := pp.GoName // fallback
			if i < len(specPathParams) {
				wireName = specPathParams[i]
			}
			td.PathParams = append(td.PathParams, ToolPathParam{
				WireName:    wireName,
				GoVarName:   pp.GoName,
				GoType:      pp.GoType,
				Description: opMeta.ParamDescs[wireName],
			})
		}

		// Collect body field wire names for collision detection
		bodyWireNames := map[string]bool{}

		// Resolve body params (if JSON body)
		if method.BodyTypeName != "" {
			bodyFields, ok := inspect.ResolveBodyStruct(method.BodyTypeName)
			if ok {
				for _, f := range bodyFields {
					bodyWireNames[f.WireName] = true
					td.BodyParams = append(td.BodyParams, ToolParamData{
						WireName:    f.WireName,
						GoFieldName: f.GoName,
						GoType:      f.GoType,
						MCPType:     f.MCPType,
						Required:    f.Required,
						Description: opMeta.BodyDescs[f.WireName],
						IsArray:     f.MCPType == "Array",
					})
				}
			}
		}

		// Resolve query params (if params struct exists)
		if method.ParamsTypeName != "" {
			if fields, ok := inspect.Structs[method.ParamsTypeName]; ok {
				for _, f := range fields {
					// Skip params that collide with body fields (body wins)
					if bodyWireNames[f.WireName] {
						continue
					}
					td.QueryParams = append(td.QueryParams, ToolParamData{
						WireName:    f.WireName,
						GoFieldName: f.GoName,
						GoType:      f.GoType,
						MCPType:     f.MCPType,
						Required:    f.Required,
						Description: opMeta.ParamDescs[f.WireName],
						IsArray:     f.MCPType == "Array",
					})
				}
			}
		}

		// Build AllParams: path params (as ToolParamData) + query + body, deduplicated
		seen := map[string]bool{}
		for _, pp := range td.PathParams {
			td.AllParams = append(td.AllParams, ToolParamData{
				WireName:    pp.WireName,
				MCPType:     "String", // path params are always strings
				Required:    true,     // path params are always required
				Description: pp.Description,
			})
			seen[pp.WireName] = true
		}
		for _, qp := range td.QueryParams {
			if !seen[qp.WireName] {
				td.AllParams = append(td.AllParams, qp)
				seen[qp.WireName] = true
			}
		}
		for _, bp := range td.BodyParams {
			if !seen[bp.WireName] {
				td.AllParams = append(td.AllParams, bp)
				seen[bp.WireName] = true
			}
		}

		tools = append(tools, td)
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].OperationID < tools[j].OperationID
	})

	return tools
}

// specPathParamNames extracts path parameter wire names from the spec metadata,
// ordered by their appearance in the path pattern.
func specPathParamNames(meta *OperationMeta) []string {
	// Extract {param_name} tokens from path in order
	var names []string
	path := meta.Path
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			break
		}
		names = append(names, path[start+1:start+end])
		path = path[start+end+1:]
	}
	return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestMergeToolData -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/merge.go internal/codegen/merge_test.go
git commit -m "feat(codegen): add merge logic joining AST and metadata [SPO-46]"
```

---

## Task 4: Code Generator Rewrite

**Files:**
- Rewrite: `internal/codegen/tools_gen.go`
- Rewrite: `internal/codegen/tools_gen_test.go`

### Overview

Replace the single-file template with per-endpoint file generation. Each generated file contains a tool definition, scopes, and handler. An aggregator file contains `AllTools`, `AllRegistrations`, `AllScopes`, and type conversion helpers.

The existing helper functions (`kebabToCamel`, `snakeToCamel`, `snakeToPascal`, `toolDescription`, `mcpType`, `goReserved`) are retained in `tools_gen.go` as they're reused by other modules.

- [ ] **Step 1: Write failing test for per-file generation**

Rewrite `internal/codegen/tools_gen_test.go`:

```go
package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToolFiles(t *testing.T) {
	clientSrc := loadFixture(t, "fixture_client.go")
	typesSrc := loadFixture(t, "fixture_types.go")
	specData := loadFixture(t, "spotify_fixture.yaml")

	inspect, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	meta, err := ExtractMetadata(specData)
	require.NoError(t, err)

	tools := MergeToolData(inspect, meta)

	outDir := t.TempDir()
	err = GenerateToolFiles(tools, "tools", meta.ServerURL, outDir)
	require.NoError(t, err)

	t.Run("per-endpoint files created", func(t *testing.T) {
		for _, td := range tools {
			filename := "generated_tool_" + snakeCase(td.OperationID) + ".go"
			path := filepath.Join(outDir, filename)
			_, err := os.Stat(path)
			assert.NoError(t, err, "missing file: %s", filename)
		}
	})

	t.Run("aggregator file created", func(t *testing.T) {
		path := filepath.Join(outDir, "generated_tools_all.go")
		_, err := os.Stat(path)
		assert.NoError(t, err)
	})

	t.Run("per-endpoint file contains tool definition and handler", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_get_playlist.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "var GetPlaylistTool = mcp.NewTool(")
		assert.Contains(t, code, `"get-playlist"`)
		assert.Contains(t, code, "func NewGetPlaylistHandler(")
		assert.Contains(t, code, "GetPlaylistToolScopes")
		assert.Contains(t, code, `mcp.WithString("playlist_id"`)
		assert.Contains(t, code, "mcp.Required()")
		// Handler uses typed params struct
		assert.Contains(t, code, "params := &spotify.GetPlaylistParams{}")
	})

	t.Run("create-playlist has typed body assignment", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_create_playlist.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "var body spotify.CreatePlaylistJSONRequestBody")
		// Direct field assignment, no map[string]interface{} round-trip
		assert.NotContains(t, code, "map[string]interface{}")
		assert.Contains(t, code, "body.Name")
	})

	t.Run("aggregator has AllRegistrations", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tools_all.go"))
		require.NoError(t, err)
		code := string(data)

		assert.Contains(t, code, "func AllRegistrations()")
		assert.Contains(t, code, "func AllScopes()")
		assert.Contains(t, code, "var AllTools")
		assert.Contains(t, code, "func toStringSlice(")
		assert.Contains(t, code, "func toInt(")
	})

	t.Run("no duplicate param declarations", func(t *testing.T) {
		// Search has both q and type params, none duplicated
		data, err := os.ReadFile(filepath.Join(outDir, "generated_tool_search.go"))
		require.NoError(t, err)
		code := string(data)

		// Count occurrences of WithString("q"
		count := countOccurrences(code, `mcp.WithString("q"`)
		assert.Equal(t, 1, count, "param 'q' should appear exactly once")
	})
}

func countOccurrences(s, sub string) int {
	count := 0
	for i := 0; ; {
		idx := indexOf(s[i:], sub)
		if idx < 0 {
			break
		}
		count++
		i += idx + len(sub)
	}
	return count
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestGenerateToolFiles -v`

Expected: FAIL with "undefined: GenerateToolFiles"

- [ ] **Step 3: Implement `GenerateToolFiles`**

Rewrite `internal/codegen/tools_gen.go`. Keep the existing helper functions (`kebabToCamel`, `snakeToCamel`, `snakeToPascal`, `toolDescription`, `mcpType`, `goReserved`) and replace everything else.

The new file should contain:

1. `GenerateToolFiles(tools []ToolData, packageName, serverURL, outputDir string) error` - main entry point
2. `generateToolFile(td ToolData, packageName string) (string, error)` - renders one per-endpoint file
3. `generateAggregatorFile(tools []ToolData, packageName, serverURL string) (string, error)` - renders the aggregator
4. `snakeCase(kebab string) string` - converts `kebab-case` to `snake_case` for filenames
5. Per-endpoint template (`toolFileTmpl`)
6. Aggregator template (`aggregatorTmpl`)

**Per-endpoint template** (`toolFileTmpl`):

```
// Code generated by cmd/codegen; DO NOT EDIT.

package {{.PackageName}}

import (
	"context"
	"fmt"
{{- if .Tool.IsNonJSONBody}}
	"strings"
{{- end}}

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

// {{.Tool.VarName}}Scopes lists the OAuth scopes required by the {{.Tool.OperationID}} tool.
var {{.Tool.VarName}}Scopes = []string{ {{- range .Tool.Scopes}}{{quote .}}, {{end -}} }

var {{.Tool.VarName}} = mcp.NewTool({{quote .Tool.OperationID}},
	mcp.WithDescription({{quote .Tool.Description}}),
{{- range .Tool.AllParams}}
	mcp.With{{.MCPType}}({{quote .WireName}}{{if .Required}}, mcp.Required(){{end}}{{if .Description}}, mcp.Description({{quote .Description}}){{end}}),
{{- end}}
)

// {{.Tool.HandlerName}} creates a handler for the {{.Tool.OperationID}} tool.
func {{.Tool.HandlerName}}(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
{{- range .Tool.PathParams}}
		{{.GoVarName}}, _ := args[{{quote .WireName}}].(string)
{{- if ne .GoType "string"}}
		{{.GoVarName}}Typed := spotify.{{.GoType}}({{.GoVarName}})
{{- end}}
{{- end}}
{{- if .Tool.ParamsTypeName}}
		params := &spotify.{{.Tool.ParamsTypeName}}{}
{{- range .Tool.QueryParams}}
{{- if and .Required .IsArray}}
		if v, ok := args[{{quote .WireName}}]; ok {
			params.{{.GoFieldName}} = toTypedSlice[spotify.{{sliceElemType .GoType}}](v)
		}
{{- else if .Required}}
		params.{{.GoFieldName}} = {{assignRequired . "args"}}
{{- else if .IsArray}}
		if v, ok := args[{{quote .WireName}}]; ok {
			sl := toStringSlice(v)
			params.{{.GoFieldName}} = sl
		}
{{- else}}
		{{assignOptional . "args" "params"}}
{{- end}}
{{- end}}
{{- end}}
{{- if .Tool.HasJSONBody}}
		var body spotify.{{.Tool.BodyTypeName}}
{{- range .Tool.BodyParams}}
{{- if .IsArray}}
		if v, ok := args[{{quote .WireName}}]; ok {
{{- if isPointer .GoType}}
			sl := toStringSlice(v)
			body.{{.GoFieldName}} = &sl
{{- else}}
			body.{{.GoFieldName}} = toStringSlice(v)
{{- end}}
		}
{{- else}}
		{{assignBodyField . "args"}}
{{- end}}
{{- end}}
{{- end}}

{{- if .Tool.IsNonJSONBody}}
		resp, err := client.{{.Tool.CamelName}}WithBodyWithResponse(ctx{{range .Tool.PathParams}}, {{pathArgExpr .}}{{end}}, {{quote .Tool.BodyContentType}}, strings.NewReader(req.GetString("body", "")))
{{- else}}
		resp, err := client.{{.Tool.CamelName}}WithResponse(ctx{{range .Tool.PathParams}}, {{pathArgExpr .}}{{end}}{{if .Tool.ParamsTypeName}}, params{{end}}{{if .Tool.HasJSONBody}}, body{{end}})
{{- end}}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if resp.HTTPResponse.StatusCode >= 400 {
			return mcp.NewToolResultError(fmt.Sprintf("Spotify API error %d: %s", resp.HTTPResponse.StatusCode, string(resp.Body))), nil
		}

		return mcp.NewToolResultText(string(resp.Body)), nil
	}
}
```

**Aggregator template** (`aggregatorTmpl`):

```
// Code generated by cmd/codegen; DO NOT EDIT.

package {{.PackageName}}

import (
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)
{{- if .ServerURL}}

const ServerURL = {{quote .ServerURL}}
{{- end}}

// AllTools contains all generated MCP tool definitions.
var AllTools = []mcp.Tool{
{{- range .Tools}}
	{{.VarName}},
{{- end}}
}

// AllRegistrations returns all generated tool registrations paired with their handler factories.
func AllRegistrations() []ToolRegistration {
	return []ToolRegistration{
{{- range .Tools}}
		{Tool: {{.VarName}}, NewHandler: {{.HandlerName}}},
{{- end}}
	}
}

// AllScopes returns the deduplicated, sorted union of all OAuth scopes required by all tools.
func AllScopes() []string {
	seen := make(map[string]bool)
{{- range .Tools}}
	for _, s := range {{.VarName}}Scopes {
		seen[s] = true
	}
{{- end}}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}

// Type conversion helpers for MCP args → Go types.

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func toTypedSlice[T ~string](v interface{}) []T {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]T, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, T(s))
		}
	}
	return result
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func toBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}
```

The template helper functions registered via `template.FuncMap`:

- `quote(s string) string` - wraps in Go double quotes
- `isPointer(goType string) bool` - checks if type starts with `*`
- `sliceElemType(goType string) string` - extracts element type from `[]Foo` or `*[]Foo`
- `pathArgExpr(pp ToolPathParam) string` - returns `varName` or `varNameTyped` if type is not string
- `assignRequired(p ToolParamData, argsVar string) string` - generates type assertion for required fields
- `assignOptional(p ToolParamData, argsVar, structVar string) string` - generates if-let for optional fields
- `assignBodyField(p ToolParamData, argsVar string) string` - generates typed body field assignment

These template functions produce the correct type assertions and assignments based on MCPType and Required:

For **assignRequired**:
- String: `toType(args["name"].(string))` or just `args["name"].(string)`
- Number (int): `toInt(args["name"])`
- Boolean: `toBool(args["name"])`

For **assignOptional**:
- String: `if v, ok := args["name"].(string); ok && v != "" { params.Field = &v }`
- Number: `if v, ok := args["name"]; ok { n := toInt(v); params.Field = &n }`
- Boolean: `if v, ok := args["name"]; ok { b := toBool(v); params.Field = &b }`

For **assignBodyField**:
- String (required): `body.Field = args["name"].(string)`
- String (optional, pointer): `if v, ok := args["name"].(string); ok && v != "" { body.Field = &v }`
- Number (optional, pointer): `if v, ok := args["name"]; ok { n := toInt(v); body.Field = &n }`
- Boolean (optional, pointer): `if v, ok := args["name"]; ok { b := toBool(v); body.Field = &b }`
- Object (map): `if v, ok := args["name"]; ok { if m, ok := v.(*map[string]interface{}); ok { body.Field = m } }`

**Implementation note:** The template functions should generate multi-line strings where needed. Use `text/template` with proper indentation. The `go/format` pass will normalize formatting.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestGenerateToolFiles -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/tools_gen.go internal/codegen/tools_gen_test.go
git commit -m "feat(codegen): rewrite tool generator for per-file AST-based output [SPO-46]"
```

---

## Task 5: Update Pipeline and Delete Old Code

**Files:**
- Modify: `cmd/codegen/main.go`
- Delete: `internal/codegen/parser.go`, `internal/codegen/parser_test.go`
- Delete: `internal/codegen/sanitize.go`
- Delete: `internal/codegen/scopes.go`, `internal/codegen/scopes_test.go`
- Delete: `internal/codegen/generate_integration_test.go`
- Delete: `internal/tools/generated_tools.go`

- [ ] **Step 1: Update `cmd/codegen/main.go`**

Replace the `run()` function with the new pipeline:

```go
func run() error {
	specURL := os.Getenv("SPOTIFY_OPENAPI_SPEC_URL")
	if specURL == "" {
		specURL = defaultSpecURL
	}

	projectRoot := "."
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
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
	oapiConfig.ClientOutput = filepath.Join(projectRoot, oapiConfig.ClientOutput)
	oapiConfig.TypesOutput = filepath.Join(projectRoot, oapiConfig.TypesOutput)

	toolsDir := filepath.Join(projectRoot, "internal", "tools")

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

	// Step 2: Generate Spotify client (oapi-codegen) - unchanged
	fmt.Println("Generating Spotify client...")
	if err := codegen.GenerateFromSpec(specData, oapiConfig); err != nil {
		return fmt.Errorf("generating client: %w", err)
	}
	fmt.Printf("  %s\n", oapiConfig.ClientOutput)
	fmt.Printf("  %s\n", oapiConfig.TypesOutput)

	// Step 3: AST inspect generated files
	fmt.Println("Inspecting generated client types...")
	clientSrc, err := os.ReadFile(oapiConfig.ClientOutput)
	if err != nil {
		return fmt.Errorf("reading client file: %w", err)
	}
	typesSrc, err := os.ReadFile(oapiConfig.TypesOutput)
	if err != nil {
		return fmt.Errorf("reading types file: %w", err)
	}
	inspectResult, err := codegen.Inspect(clientSrc, typesSrc)
	if err != nil {
		return fmt.Errorf("inspecting client types: %w", err)
	}
	fmt.Printf("  %d methods, %d structs\n", len(inspectResult.Methods), len(inspectResult.Structs))

	// Step 4: Extract metadata from spec
	fmt.Println("Extracting metadata from spec...")
	metaResult, err := codegen.ExtractMetadata(specData)
	if err != nil {
		return fmt.Errorf("extracting metadata: %w", err)
	}
	fmt.Printf("  %d operations\n", len(metaResult.Operations))

	// Step 5: Merge and generate tool files
	fmt.Println("Generating MCP tool files...")
	tools := codegen.MergeToolData(inspectResult, metaResult)
	fmt.Printf("  %d tools\n", len(tools))

	if err := codegen.GenerateToolFiles(tools, "tools", metaResult.ServerURL, toolsDir); err != nil {
		return fmt.Errorf("generating tool files: %w", err)
	}
	fmt.Printf("  %d files written to %s\n", len(tools)+1, toolsDir)

	fmt.Println("Done.")
	return nil
}
```

- [ ] **Step 2: Delete old files**

```bash
rm internal/codegen/parser.go
rm internal/codegen/parser_test.go
rm internal/codegen/sanitize.go
rm internal/codegen/scopes.go
rm internal/codegen/scopes_test.go
rm internal/codegen/generate_integration_test.go
rm internal/tools/generated_tools.go
```

- [ ] **Step 3: Update `generate.go` to remove `SanitizeSpec` dependency**

The `GenerateFromSpec` function in `generate.go` calls `SanitizeSpec`. Since we're deleting `sanitize.go`, we need to update `generate.go` to pass raw spec data directly to kin-openapi (which handles the quirks natively). Replace:

```go
sanitized, err := SanitizeSpec(specData)
```

with:

```go
sanitized := specData
```

Actually, kin-openapi does NOT handle the `required: true` boolean quirk natively for oapi-codegen. The sanitization was needed for oapi-codegen's input, not for our parser. Check if the oapi-codegen generation still works without sanitization by running:

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/codegen/ -run TestGenerate -v
```

If it fails, keep a minimal sanitization inline in `generate.go` rather than as a separate file. If it passes, remove the call entirely.

**Important:** The `generate_test.go` file tests `GenerateFromSpec` with the fixture YAML. Run those tests to verify they still pass after removing `SanitizeSpec`. If they fail because the fixture uses the Swagger 2.0 `required: true` pattern, update the fixture to use valid OpenAPI 3.0 format OR keep a slimmed-down sanitization function in `generate.go`.

- [ ] **Step 4: Verify codegen package compiles**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go build ./internal/codegen/`

Expected: compiles successfully

- [ ] **Step 5: Verify tools package compiles (it won't yet, that's expected)**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go build ./internal/tools/`

Expected: FAIL (generated_tools.go is deleted, new files not generated yet)

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(codegen): wire new pipeline and delete old parser/sanitizer [SPO-46]"
```

---

## Task 6: Generate New Tool Files

**Files:**
- Generate: `internal/tools/generated_tool_*.go` (~48 files)
- Generate: `internal/tools/generated_tools_all.go`
- Modify: `go.mod` (go mod tidy)

- [ ] **Step 1: Run the codegen against the real Spotify spec**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go run ./cmd/codegen
```

This fetches the live spec, generates the Spotify client, inspects it via AST, extracts metadata, merges, and writes per-endpoint tool files.

Expected: completes successfully, prints tool count.

- [ ] **Step 2: Verify generated files exist**

```bash
ls internal/tools/generated_tool_*.go | wc -l
ls internal/tools/generated_tools_all.go
```

Expected: ~48 per-endpoint files + 1 aggregator file.

- [ ] **Step 3: Verify the tools package compiles**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go build ./internal/tools/
```

Expected: compiles successfully.

- [ ] **Step 4: Verify existing tests pass**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/tools/ -v
```

Expected: PASS (registry_test.go should still pass since it uses its own test registrations).

- [ ] **Step 5: Run go mod tidy**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go mod tidy
```

- [ ] **Step 6: Run full test suite**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && make test
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(codegen): generate per-endpoint tool files from AST pipeline [SPO-46]"
```

---

## Task 7: Integration Test with gock

**Files:**
- Modify: `go.mod` (add gock dependency)
- Create: `internal/tools/generated_tools_integration_test.go`

### Overview

Dynamic integration test that iterates every registered tool, builds synthetic args from `InputSchema`, sets up gock to intercept the expected Spotify API request, calls the handler, and asserts the outgoing HTTP request has the correct path, query string, and JSON body. Dedicated sub-test for `add-items-to-playlist` verifying array `uris` in body.

- [ ] **Step 1: Add gock dependency**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go get gopkg.in/h2non/gock.v1 && go mod tidy
```

- [ ] **Step 2: Write the integration test**

Write `internal/tools/generated_tools_integration_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"

	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

// syntheticArgs builds MCP arguments from a tool's InputSchema.
// String -> "test", Number -> 1, Array -> ["test"], Boolean -> true
func syntheticArgs(tool mcp.Tool) map[string]interface{} {
	args := make(map[string]interface{})
	props, ok := tool.InputSchema.Properties.(map[string]interface{})
	if !ok {
		return args
	}
	for name, raw := range props {
		prop, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch prop["type"] {
		case "string":
			args[name] = "test"
		case "number", "integer":
			args[name] = float64(1)
		case "array":
			args[name] = []interface{}{"test"}
		case "boolean":
			args[name] = true
		default:
			args[name] = "test"
		}
	}
	return args
}

func TestAllToolsHTTPIntegration(t *testing.T) {
	defer gock.Off()

	regs := AllRegistrations()
	require.NotEmpty(t, regs, "AllRegistrations should return tools")

	for _, reg := range regs {
		toolName := reg.Tool.Name
		t.Run(toolName, func(t *testing.T) {
			gock.Clean()
			defer gock.Off()

			// Create a client pointing at gock's intercepted URL
			httpClient := &http.Client{Transport: gock.DefaultTransport}
			client, err := spotify.NewClientWithResponses(
				ServerURL,
				spotify.WithHTTPClient(httpClient),
			)
			require.NoError(t, err)

			handler := reg.NewHandler(client)

			args := syntheticArgs(reg.Tool)

			// Set up gock to intercept any request to the Spotify API
			var capturedReq *http.Request
			gock.New(ServerURL).
				AddMatcher(func(req *http.Request, ereq *gock.Request) (bool, error) {
					capturedReq = req
					return true, nil
				}).
				Reply(200).
				JSON(map[string]string{"status": "ok"})

			req := mcp.CallToolRequest{}
			req.Params.Name = toolName
			req.Params.Arguments = args

			result, err := handler(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.IsError, "tool %s returned error: %v", toolName, result)

			// Verify a request was made
			require.NotNil(t, capturedReq, "no HTTP request captured for tool %s", toolName)
		})
	}
}

func TestAddItemsToPlaylistBodyUris(t *testing.T) {
	defer gock.Off()

	httpClient := &http.Client{Transport: gock.DefaultTransport}
	client, err := spotify.NewClientWithResponses(
		ServerURL,
		spotify.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	handler := NewAddItemsToPlaylistHandler(client)

	var capturedReq *http.Request
	gock.New(ServerURL).
		AddMatcher(func(req *http.Request, ereq *gock.Request) (bool, error) {
			capturedReq = req
			return true, nil
		}).
		Reply(200).
		JSON(map[string]string{"snapshot_id": "abc"})

	args := map[string]interface{}{
		"playlist_id": "test-playlist-123",
		"uris":        []interface{}{"spotify:track:abc", "spotify:track:def"},
		"position":    float64(0),
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "add-items-to-playlist"
	req.Params.Arguments = args

	result, err := handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	// Verify the request
	require.NotNil(t, capturedReq)

	// Path should contain the playlist ID
	assert.True(t, strings.Contains(capturedReq.URL.Path, "test-playlist-123"),
		"path should contain playlist_id, got: %s", capturedReq.URL.Path)

	// Body should contain uris as an array, NOT in query string
	assert.Empty(t, capturedReq.URL.Query().Get("uris"),
		"uris should NOT be in query string")

	// Parse the request body
	var body map[string]interface{}
	err = json.NewDecoder(capturedReq.Body).Decode(&body)
	require.NoError(t, err, "request body should be valid JSON")

	uris, ok := body["uris"]
	require.True(t, ok, "body should contain 'uris' field")

	urisArr, ok := uris.([]interface{})
	require.True(t, ok, "uris should be an array")
	assert.Len(t, urisArr, 2)
	assert.Equal(t, "spotify:track:abc", urisArr[0])
	assert.Equal(t, "spotify:track:def", urisArr[1])
}
```

- [ ] **Step 3: Run the integration test**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/tools/ -run TestAllToolsHTTPIntegration -v -count=1`

Expected: PASS for all tools.

- [ ] **Step 4: Run the add-items-to-playlist specific test**

Run: `cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && go test ./internal/tools/ -run TestAddItemsToPlaylistBodyUris -v`

Expected: PASS. Verifies `uris` is an array in the JSON body, not in the query string.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/generated_tools_integration_test.go go.mod go.sum
git commit -m "test(tools): add gock-based integration test for all tool handlers [SPO-46]"
```

---

## Task 8: Final Verification

- [ ] **Step 1: Run make lint**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && make lint
```

Expected: PASS. Fix any lint errors before proceeding.

- [ ] **Step 2: Run make test**

```bash
cd /Users/gregory.orton/Personal/makesometh.ing/spotify-mcp-go && make test
```

Expected: PASS. All tests green.

- [ ] **Step 3: Verify acceptance criteria**

| Criterion | Evidence |
|-----------|----------|
| AST-based parameter mapping | `inspector.go` parses `ClientWithResponses` methods |
| No `map[string]interface{}` round-trip | Generated handlers use typed struct fields directly |
| One file per endpoint | `internal/tools/generated_tool_*.go` |
| Dynamic integration test | `TestAllToolsHTTPIntegration` iterates `AllRegistrations()` |
| gock-based, CI-friendly | No external deps, runs via `make test` |
| `add-items-to-playlist` array uris | `TestAddItemsToPlaylistBodyUris` |
| No duplicate params | Template uses `AllParams` with dedup logic |
| No regressions | `registry_test.go` still passes, all tools respond |

- [ ] **Step 4: Commit any remaining fixes**

If lint or tests required fixes, commit them:

```bash
git add -A
git commit -m "fix(codegen): address lint and test issues [SPO-46]"
```
