# AST-Based Codegen Overhaul [SPO-46]

## Problem

The MCP tool codegen pipeline (`parser.go` + `tools_gen.go`) is a hand-rolled OpenAPI YAML parser that re-derives parameter types, locations, and body schemas from raw YAML. This duplicates work already done correctly by oapi-codegen. Every discrepancy between our parser's interpretation and oapi-codegen's is a runtime bug (SPO-44, SPO-45).

## Decision

Use the generated client types as the single source of truth for parameter mapping. `go/ast` inspects `generated_client.go` and `generated_types.go` to derive parameter names, types, locations, and required/optional status. kin-openapi provides metadata only (descriptions, scopes, deprecated flags).

`go/ast` over `go/types`: we only need struct field tags and method signatures from files in one package. No cross-package type resolution needed. `go/types` would require loading the full module graph for no benefit.

kin-openapi over hand-rolled YAML: it's already an indirect dependency via oapi-codegen, handles Swagger 2.0 quirks natively, and lets us delete `parser.go` and `sanitize.go`.

## Architecture

```
oapi-codegen → generated_client.go + generated_types.go  (unchanged, step 1)
                         |
              go/ast inspects those files                  (new, step 2a)
              kin-openapi reads spec for metadata           (new, step 2b)
                         |
              per-method file generation                    (new, step 3)
```

## AST Inspector

Parses `generated_client.go` and `generated_types.go`. For each `ClientWithResponses` method ending in `WithResponse`:

### Method signature classification

Parameters after `context.Context` and before `...RequestEditorFn`:

- Type matches `*<Name>Params` → query params struct (look up struct fields)
- Type matches `<Name>JSONRequestBody` → body type (resolve alias, look up struct fields)
- Everything else → path param

### Param struct inspection

For `*XxxParams` structs, walk fields and read `form:` tags:

- Pointer type = optional, value type = required
- Go type mapping: `string`→String, `int`/`float64`→Number, `bool`→Boolean, `[]T`→Array

### Body type inspection

For `XxxJSONRequestBody`, resolve the type alias to find the underlying struct, walk fields, read `json:` tags. Same pointer/required and type mapping rules.

### Name collision handling

When a param name appears in both query and body (e.g., `uris` in add-items-to-playlist), the body version wins. The handler only populates the body field. Body is the canonical location for POST/PUT/PATCH; the query version is legacy.

## Metadata Extraction

kin-openapi loads the filtered spec. For each non-deprecated operation:

- `Summary` + `Description` → tool description
- Per-parameter `Description` → param descriptions in tool definition
- Per-body-property `Description` → same
- `Security` requirements → OAuth scopes
- `Servers[0].URL` → API base URL constant

## Handler Generation

No `map[string]interface{}` round-trip. Direct typed field assignment:

```go
func NewAddItemsToPlaylistHandler(client *spotify.ClientWithResponses) server.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        args := req.GetArguments()

        // Path params
        playlistId, _ := args["playlist_id"].(string)

        // Query params (typed struct, body-colliding names excluded)
        params := &spotify.AddItemsToPlaylistParams{}

        // Body params (typed struct, direct field assignment)
        var body spotify.AddItemsToPlaylistJSONRequestBody
        if v, ok := args["uris"]; ok {
            body.Uris = toStringSlice(v)
        }
        if v, ok := args["position"]; ok {
            n := toInt(v)
            body.Position = &n
        }

        resp, err := client.AddItemsToPlaylistWithResponse(ctx, playlistId, params, body)
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }
        return mcp.NewToolResultText(string(resp.Body)), nil
    }
}
```

Type conversion helpers as package-level funcs:

- `toStringSlice(v interface{}) []string`
- `toInt(v interface{}) int`
- `toFloat(v interface{}) float64`
- `toBool(v interface{}) bool`

## File-Per-Method Output

```
internal/tools/
  registry.go                                    # Unchanged (hand-written)
  generated_tool_add_items_to_playlist.go        # One per endpoint
  generated_tool_get_playlist.go
  generated_tool_search.go
  ...
  generated_tools_all.go                         # Aggregator
```

Each `generated_tool_*.go`:

1. Tool definition (`var XxxTool = mcp.NewTool(...)`)
2. Scopes (`var XxxToolScopes = []string{...}`)
3. Handler factory (`func NewXxxHandler(...)`)

Aggregator `generated_tools_all.go`:

1. `var AllTools`
2. `AllRegistrations()`
3. `AllScopes()`
4. Type conversion helpers

## Integration Test

Uses gock for HTTP interception. Dynamically iterates every registered tool:

1. `AllRegistrations()` discovers all tools
2. For each, read `InputSchema` to get parameter names and types
3. Build synthetic args matching the schema
4. gock intercepts the outgoing Spotify API request
5. Assert correct path, query string, JSON body, HTTP method
6. Dedicated sub-test for `add-items-to-playlist` with array `uris` body param

No mock server, no credentials, runs in CI via `make test`.

## Deletions

| File | Lines | Reason |
|------|-------|--------|
| `internal/codegen/parser.go` | 299 | Replaced by AST inspector + kin-openapi |
| `internal/codegen/sanitize.go` | 179 | kin-openapi handles spec quirks |
| `internal/codegen/scopes.go` | 20 | Absorbed into metadata extraction |
| `internal/tools/generated_tools.go` | 1890 | Replaced by per-method files + aggregator |

`internal/codegen/tools_gen.go` rewritten from scratch.

## Acceptance Criteria

- [ ] Tool handler codegen derives parameter mapping from generated client types via AST inspection, not from raw OpenAPI YAML parsing
- [ ] No `map[string]interface{}` round-trip for JSON body construction; handlers assign typed struct fields directly
- [ ] One generated file per endpoint, each containing tool definition + handler
- [ ] Dynamic integration test iterates `AllRegistrations()`, builds params from `InputSchema`, and asserts correct HTTP request (path, query, body) for every tool
- [ ] Integration test uses gock for HTTP interception, runs in CI (`make test`) with no external dependencies or credentials
- [ ] `add-items-to-playlist` specifically tested with array `uris` body param (the original bug)
- [ ] No duplicate param declarations in tool definitions
- [ ] Existing tool behavior preserved (no regressions)
