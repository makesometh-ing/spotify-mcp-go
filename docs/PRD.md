# Spotify MCP Server in Go - PRD

## Overview

A Model Context Protocol (MCP) server that exposes Spotify's Web API as MCP tools. Built in Go using the mcp-go library. The server implements the MCP OAuth spec as a proxy to Spotify, so MCP clients (Claude Desktop, Claude Code, Codex, Cursor) handle the browser-based login flow automatically.

A companion code generator fetches Spotify's OpenAPI spec and generates the Go source files for both the Spotify API client and the MCP tool definitions. This runs in CI on a weekly cron to keep the tool surface area current.

### Why a Proxy

Spotify does not support OAuth Dynamic Client Registration (RFC 7591). MCP clients (Claude Desktop, etc.) expect to register themselves dynamically with the MCP server's authorization endpoint. Since they can't register directly with Spotify, our MCP server bridges this gap: it acts as an OAuth authorization server to MCP clients and as an OAuth client to Spotify, using the operator's pre-registered Spotify app credentials.

This pattern was validated by examining the Datadog MCP server at `mcp.datadoghq.com`, which implements the same proxy architecture: well-known metadata discovery, dynamic client registration, and proxied authorize/token endpoints.

### Why HTTP-Only (No stdio)

The MCP OAuth flow requires the server to expose HTTP endpoints: `/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`, `/register`, `/authorize`, `/callback`, `/token`. These cannot be served over stdio. The server uses MCP's Streamable HTTP transport exclusively.

## System Components

Three artifacts from one repo:

1. **`spotify-mcp-go`** (cmd/server) - The MCP server. Streamable HTTP transport only. Handles MCP protocol, OAuth proxy to Spotify, tool dispatch. Distributed to users.

2. **`codegen`** (cmd/codegen) - Internal maintainer tool. Fetches the OpenAPI spec, filters out deprecated endpoints, runs oapi-codegen for the Go client, and generates MCP tool definitions. Run in CI, output committed to the repo. Not distributed.

3. **Container image** - Built with `ko` (no Dockerfile). Wraps `spotify-mcp-go` for containerized deployment.

### Build Tooling

- `make` for build orchestration (`make build`, `make codegen`, `make docker`, `make run`)
- `ko` for container images
- GitHub Actions for CI (weekly codegen cron, release on merge)

### Environment Variables

**MCP server:**

| Variable | Required | Default | Description |
|---|---|---|---|
| `SPOTIFY_CLIENT_ID` | Yes | - | Spotify app client ID (user registers at developer.spotify.com) |
| `SPOTIFY_CLIENT_SECRET` | Yes | - | Spotify app client secret |
| `SPOTIFY_MCP_PORT` | No | `8080` | HTTP server port |
| `SPOTIFY_MCP_TOKEN_DB` | No | `~/.config/spotify-mcp-go/auth/tokens.db` | SQLite token storage path |
| `SPOTIFY_MCP_BASE_URL` | No | `http://127.0.0.1:<port>` | Public base URL for hosting behind a reverse proxy or tunnel. When set, all OAuth metadata, callback redirects, and startup output use this URL. |

The server reads from a `.env` file in the working directory if present. Configuration precedence: CLI flags > environment variables > `.env` file > defaults. A `.env.example` file is included in the repo.

### CLI Flags

| Flag | Env Fallback | Default | Description |
|---|---|---|---|
| `--spotify-client-id` | `SPOTIFY_CLIENT_ID` | (required) | Spotify app client ID |
| `--spotify-client-secret` | `SPOTIFY_CLIENT_SECRET` | (required) | Spotify app client secret |
| `--port` | `SPOTIFY_MCP_PORT` | `8080` | HTTP server port |
| `--token-db` | `SPOTIFY_MCP_TOKEN_DB` | `~/.config/spotify-mcp-go/auth/tokens.db` | SQLite token storage path |
| `--base-url` | `SPOTIFY_MCP_BASE_URL` | `http://127.0.0.1:<port>` | Public base URL for reverse proxy/tunnel |
| `--debug` | - | `false` | Enable debug logging to stderr |

### Logging

The server uses [Uber Zap](https://github.com/uber-go/zap) with the SugaredLogger for readable, structured output.

**Behavior:**

- **Default (no `--debug`):** No log output. The server is silent except for the startup banner printed to stdout.
- **`--debug` enabled:** Pretty-printed logs emitted to stderr at DEBUG level and above. Format: Zap's console encoder (not JSON) with colorized log levels, ISO 8601 timestamps, caller location (file:line), and message. This is a CLI tool, not a service; logs must be human-scannable in a terminal.

**What gets logged (when `--debug` is on):**

- OAuth flow events: `/authorize` requests, `/callback` processing, token exchanges, token refreshes, registration
- HTTP requests: method, path, status code, duration
- MCP tool invocations: tool name, input arguments, duration, success/failure, output response
- Spotify API calls: endpoint, status code, duration
- Token store operations: store, load, delete, expiry cleanup
- Server lifecycle: startup, shutdown, listener address

**Integration pattern:**

- A `*zap.SugaredLogger` is created in `cmd/server/main.go` based on the `--debug` flag. When debug is off, use `zap.NewNop()`. When on, use `zap.NewDevelopment()` with console encoding.
- The logger is passed down through constructors: `auth.NewHandler`, `tools.Register`, store implementations, and the Spotify client.
- Each component logs with a named sub-logger (e.g., `logger.Named("auth")`, `logger.Named("spotify")`) for easy filtering.

**Codegen (CI only):**

| Variable | Required | Default | Description |
|---|---|---|---|
| `SPOTIFY_OPENAPI_SPEC_URL` | No | `https://developer.spotify.com/reference/web-api/open-api-schema.yaml` | OpenAPI spec URL |

## Server Startup Behavior

On startup, the server must print:

1. The MCP endpoint URL (e.g., `http://127.0.0.1:8080/mcp`)
2. The callback URL that must be registered in the user's Spotify app (e.g., `http://127.0.0.1:8080/callback`)
3. A message directing the user to configure this callback URL in their Spotify Developer Dashboard at https://developer.spotify.com/dashboard under their app's Redirect URIs settings
4. Whether `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET` are set (fail fast if missing)

This is critical because the OAuth flow will fail silently if the callback URL is not registered in the Spotify app.

## OAuth Proxy Architecture

The MCP server implements the MCP OAuth spec (RFC 8414, RFC 9728, RFC 7591) as a proxy to Spotify. MCP clients never interact with Spotify directly. The server uses PKCE (S256) on both layers: MCP client to server, and server to Spotify.

**The MCP server is responsible for Spotify token lifecycle.** MCP clients manage their own MCP tokens (issued by our server). The server transparently refreshes expired Spotify tokens when handling MCP requests, using Spotify's token endpoint. The client has no awareness of Spotify token state.

### Spotify OAuth Endpoints

These are the Spotify endpoints the server proxies to (per Spotify's official developer docs and their live RFC 8414 metadata at `https://accounts.spotify.com/.well-known/oauth-authorization-server`):

| Endpoint | URL |
|---|---|
| Authorization | `https://accounts.spotify.com/authorize` |
| Token | `https://accounts.spotify.com/api/token` |
| RFC 8414 Metadata | `https://accounts.spotify.com/.well-known/oauth-authorization-server` |
| OIDC Discovery | `https://accounts.spotify.com/.well-known/openid-configuration` |

Spotify supports PKCE with S256 code challenge method and `token_endpoint_auth_methods_supported: ["none", "client_secret_basic", "client_secret_post"]`.

### MCP Server Endpoints

All served on the same host:port as the MCP endpoint. MCP clients discover the auth endpoints via the well-known URLs, which they fetch from the same origin as the MCP server URL they already have configured.

| Endpoint | Method | Purpose |
|---|---|---|
| `/mcp` | POST | MCP streamable HTTP endpoint. Returns 401 if unauthenticated. |
| `/.well-known/oauth-protected-resource` | GET | RFC 9728 Protected Resource Metadata. Points to self as authorization server. |
| `/.well-known/oauth-authorization-server` | GET | RFC 8414 Authorization Server Metadata. Advertises authorize/token/register endpoints, PKCE required (S256), grant types `authorization_code` and `refresh_token`, `token_endpoint_auth_methods_supported: ["none"]`. |
| `/register` | POST | RFC 7591 Dynamic Client Registration. Accepts JSON body with `redirect_uris`, `grant_types`, `response_types`, `token_endpoint_auth_method`, `client_name`. Issues a unique `client_id` and echoes back all registered metadata. |
| `/authorize` | GET | Redirects user to Spotify's authorize endpoint with server's Spotify credentials. |
| `/callback` | GET | Receives Spotify's OAuth callback after user login. |
| `/token` | POST | Proxies token exchange and refresh to Spotify. Issues MCP tokens to clients. |

### Authorization Flow

1. MCP client hits `POST /mcp`, gets 401.
2. Client fetches `GET /.well-known/oauth-protected-resource` from the same origin as the MCP server URL. This returns `authorization_servers` pointing to the same origin.
3. Client fetches `GET /.well-known/oauth-authorization-server` to discover endpoints.
4. Client registers via `POST /register` with a JSON body containing `redirect_uris` and optionally `grant_types`, `response_types`, `token_endpoint_auth_method`, `client_name`. Receives a unique `client_id` and all registered metadata echoed back per RFC 7591. Defaults: `grant_types=["authorization_code"]`, `response_types=["code"]`, `token_endpoint_auth_method="none"`.
5. Client sends user to `GET /authorize?redirect_uri=...&code_challenge=...&client_id=...&state=...`. The server validates that `redirect_uri` matches one of the URIs registered in step 4. The client's `state` parameter is opaque to the server and must be preserved for round-tripping back to the client in step 11.
6. Server redirects to `https://accounts.spotify.com/authorize` with its own `SPOTIFY_CLIENT_ID`, a server-side callback URI (`/callback`), PKCE code_challenge (S256), and all Spotify scopes. Server stores the client's PKCE state, redirect_uri, client_id, and the client's original `state` parameter for the pending auth.
7. User logs in at Spotify, grants permissions.
8. Spotify redirects to `GET /callback?code=...&state=...`.
9. Server exchanges the Spotify auth code for Spotify tokens (access + refresh) via `https://accounts.spotify.com/api/token`, including the PKCE code_verifier.
10. Server stores Spotify tokens in the token store, keyed by the MCP client_id.
11. Server generates its own MCP auth code, redirects to the MCP client's `redirect_uri` with `?code=<mcp_code>&state=<client_state>`. The `state` value is the client's original `state` from step 5, round-tripped unmodified. If the client did not send a `state`, it is omitted from the redirect.
12. Client exchanges the MCP auth code at `POST /token`, server issues MCP access + refresh tokens.
13. Client uses MCP access token as `Authorization: Bearer` on all `POST /mcp` requests.
14. On each MCP request, server looks up associated Spotify tokens by client_id. If the Spotify access token is expired, server uses the stored Spotify refresh token to get a new one from `https://accounts.spotify.com/api/token`, updates the store, then calls the Spotify API.
15. When the MCP access token expires, client refreshes via `POST /token` with `grant_type=refresh_token`. Server issues new MCP tokens.

### Client-to-Spotify Binding

Each dynamic client registration creates a 1:1 binding between an MCP client and a Spotify user session. Every new registration requires a fresh Spotify login. No token sharing across clients.

Storage record per client_id:

```
client_id -> {
    spotify_access_token
    spotify_refresh_token
    spotify_token_expiry
    mcp_access_token
    mcp_refresh_token
    mcp_token_expiry
    created_at
    redirect_uris              # RFC 7591 registration metadata
    grant_types
    response_types
    token_endpoint_auth_method
    client_name
}
```

Old registrations are cleaned up via TTL.

### Token Storage

Pluggable via a `TokenStore` interface:

```go
type TokenStore interface {
    Store(ctx context.Context, clientID string, tokens *TokenRecord) error
    Load(ctx context.Context, clientID string) (*TokenRecord, error)
    Delete(ctx context.Context, clientID string) error
    LoadAll(ctx context.Context) (map[string]*TokenRecord, error)
}
```

On startup, the Handler calls `LoadAll` to hydrate the in-memory `TokenManager` with persisted MCP access and refresh tokens. This ensures MCP tokens survive server restarts without requiring clients to re-authenticate with Spotify.

Ships with two implementations:

- **`SQLiteTokenStore`** (default) - Persists to `~/.config/spotify-mcp-go/auth/tokens.db`. Survives restarts.
- **`InMemoryTokenStore`** - For development/testing. Tokens lost on restart.

## Code Generator

The codegen tool (`cmd/codegen/main.go`) is an internal maintainer tool, not distributed to users.

### What It Does

1. Fetches the OpenAPI spec YAML from `$SPOTIFY_OPENAPI_SPEC_URL`.
2. Parses the spec and filters out deprecated endpoints. The OpenAPI spec marks deprecated endpoints with `deprecated: true` on the operation object. The codegen removes these operations from the spec before generation.
3. Extracts the list of required OAuth scopes from the spec's `security` definitions per endpoint. Each endpoint declares its required scopes via `security: [{ oauth_2_0: [scope1, scope2] }]`. The union of all scopes across all active endpoints is used as the set requested during authorization.
4. Runs a two-step generation pipeline:

**Step 1: Spotify API client via oapi-codegen** (off-the-shelf, `github.com/oapi-codegen/oapi-codegen` v2.6.0):
- Runs against the filtered (non-deprecated only) OpenAPI spec
- Generates typed Go client + request/response types
- One method per active endpoint (e.g., `GetPlaylist(ctx, id) (*Playlist, error)`)
- Uses `net/http` under the hood, accepts auth injection via `http.Client` transport
- Configured via oapi-codegen YAML config checked into the repo

**Step 2: MCP tool definitions via custom tool** (`cmd/codegen`):
- Reads the same filtered OpenAPI spec and generates MCP tool wiring that calls the oapi-codegen'd client
- One `mcp.Tool` per active endpoint
- Tool names derived from operationId (e.g., `get-playlist`, `search-tracks`)
- Descriptions from the OpenAPI summary/description fields
- Parameters mapped from OpenAPI path/query/body params to mcp-go tool properties (`WithString`, `WithNumber`, `WithBoolean`, `Required()`, `Description()`, etc.)
- One handler function per tool that validates args, calls the generated Spotify client method, returns the result
- Scopes per tool extracted from the spec's security definitions (for future per-tool scope enforcement if needed)

### How It Extracts from the OpenAPI Spec

The Spotify OpenAPI spec (OpenAPI 3.0.3) is structured as:

- **Servers**: The `servers` block declares the API base URL (e.g., `https://api.spotify.com/v1`). The codegen extracts `servers[0].url` and generates a `const ServerURL` in the tools package, used as the default Spotify API base URL at runtime.
- **Paths**: Each path (e.g., `/playlists/{playlist_id}`) is relative to the server URL and contains operations keyed by HTTP method (GET, PUT, POST, DELETE).
- **Operations**: Each operation has an `operationId`, `summary`, `description`, `parameters`, `requestBody`, `responses`, `tags`, `deprecated` flag, and `security` requirements.
- **Tags**: Operations are tagged by category (e.g., `Players`, `Playlists`, `Tracks`). Tags determine grouping but do not affect generation since we produce one tool per operation.
- **Security**: A single security scheme `oauth_2_0` of type `oauth2` with `authorizationCode` flow. Each operation declares which scopes it requires.
- **Deprecated flag**: Operations marked `deprecated: true` are filtered out entirely.
- **Parameters**: Path parameters, query parameters, and request body schemas are mapped to MCP tool input properties.
- **Schemas**: `$ref` references to `#/components/schemas/...` define the request/response types that oapi-codegen resolves into Go structs. Request body schemas may also be defined inline (properties directly on the media type schema without a `$ref`). The MCP tool generator resolves both forms to extract body field names, types, required status, and descriptions.

The codegen reads all of this, filters by `deprecated`, and feeds the result to oapi-codegen (step 1) and the MCP tool generator (step 2).

### CI Workflows

**Codegen (weekly cron):**

1. GitHub Action runs every Monday at 09:00 UTC (`codegen.yml`).
2. Fetches spec, runs codegen (`make codegen`).
3. If generated files differ from committed files, opens a PR with the changes.
4. Maintainer reviews and merges.

**CI (every push/PR):**

1. Runs `golangci-lint` via the official GitHub Action (`ci.yml`).
2. Runs `make test` (all tests with race detector).

**Release (tag push):**

1. Triggered by pushing a tag matching `v*` (e.g., `git tag v2026.04.0 && git push --tags`).
2. Builds cross-platform binaries: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64.
3. Builds and pushes a container image to `ghcr.io` via `ko`.
4. Creates a GitHub Release with all binaries and auto-generated release notes.
5. Updates the Homebrew tap at `makesometh-ing/homebrew-tap` with new checksums.

## Project Structure

```
spotify-mcp-go/
├── .env.example                 # Example environment variables
├── .ko.yaml                     # ko build config
├── oapi-codegen.yaml            # oapi-codegen config for Spotify client generation
├── cmd/
│   ├── server/                  # MCP server entrypoint
│   │   ├── main.go              # CLI flags, signal handling, entry point
│   │   └── server.go            # Config loading, logger init, run(), HTTP wiring
│   └── codegen/                 # Internal codegen tool
│       └── main.go
├── internal/
│   ├── auth/                    # OAuth proxy layer
│   │   ├── auth.go              # Package doc
│   │   ├── handler.go           # Well-known, /authorize, /token, /register, /callback
│   │   ├── pkce.go              # PKCE code verifier/challenge generation and validation
│   │   ├── refresh.go           # Transparent Spotify token refresh with singleflight
│   │   ├── spotify.go           # Spotify OAuth client (code exchange, refresh)
│   │   ├── tokens.go            # MCP token issuance, validation
│   │   └── store/               # TokenStore interface + implementations
│   │       ├── store.go         # Interface definition
│   │       ├── sqlite.go        # SQLite implementation (default)
│   │       ├── memory.go        # In-memory implementation
│   │       └── logging.go       # Logging decorator (wraps any TokenStore)
│   ├── spotify/                 # Spotify API client
│   │   ├── client.go            # Hand-written base client (http, auth injection)
│   │   ├── generated_client.go  # Generated by oapi-codegen
│   │   └── generated_types.go   # Generated by oapi-codegen
│   ├── tools/                   # MCP tool definitions
│   │   ├── registry.go          # Hand-written tool registration
│   │   ├── generated_tools_all.go  # Generated index of all tools and scopes
│   │   └── generated_tool_*.go  # One file per tool (generated by cmd/codegen)
│   └── codegen/                 # Code generation logic
│       ├── generate.go          # oapi-codegen orchestration + discriminator fix
│       ├── inspector.go         # Type inspection for generated client
│       ├── merge.go             # Spec merging utilities
│       ├── metadata.go          # Metadata extraction from OpenAPI spec
│       ├── sanitize.go          # Generated code sanitization
│       └── tools_gen.go         # MCP tool definition generator
├── Makefile
├── go.mod
├── go.sum
└── docs/
```

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/mark3labs/mcp-go` | v0.46.0 | MCP protocol, streamable HTTP transport, tool builder API |
| `modernc.org/sqlite` | latest | Pure-Go SQLite driver (no CGO, simpler cross-compilation + ko) |
| `go.uber.org/zap` | latest | Structured logging (SugaredLogger for debug mode) |
| `golang.org/x/sync` | latest | `singleflight` for coalescing concurrent Spotify token refreshes |
| `github.com/oapi-codegen/runtime` | v1.3.1 | Runtime support for oapi-codegen generated client |
| Go stdlib `net/http` | - | HTTP server, routing (ServeMux with method routing, Go 1.22+) |
| Go stdlib `crypto` | - | MCP token generation, PKCE verification |

**Dev/CI only:**

| Package | Purpose |
|---|---|
| `github.com/oapi-codegen/oapi-codegen` v2.6.0 | Generate Go client + types from OpenAPI spec |
| `github.com/getkin/kin-openapi` | OpenAPI spec loading and validation for codegen |
| `gopkg.in/yaml.v3` | Parse OpenAPI YAML spec in MCP tool generator |

**Test only:**

| Package | Purpose |
|---|---|
| `github.com/stretchr/testify` | Test assertions (also transitive via mcp-go) |
| `gopkg.in/h2non/gock.v1` | HTTP transport interception for Spotify API tests |

**Go version:** 1.26 in `go.mod`

## Testing Strategy

All code is developed test-first (TDD). Write tests before implementation. Integration tests are preferred over unit tests wherever possible.

### Integration Tests

These test real interactions between components. They are the primary test type for this project.

**OAuth proxy (`internal/auth/`)**:
- Full authorization flow end-to-end: register -> authorize -> callback -> token exchange -> authenticated MCP request -> token refresh
- Well-known endpoint responses match the MCP OAuth spec (RFC 8414, RFC 9728)
- `/register` returns a unique client_id per registration
- `/authorize` redirects to Spotify's authorize endpoint with correct parameters (client_id, PKCE challenge, scopes, callback URI)
- `/callback` exchanges the Spotify code and stores tokens keyed by client_id
- `/token` issues MCP tokens on code exchange and refreshes on `grant_type=refresh_token`
- Transparent Spotify token refresh: when a Spotify token is expired, the server refreshes it before calling the Spotify API, without the MCP client being aware
- 401 returned for unauthenticated `/mcp` requests
- 1:1 binding enforced: each client_id maps to exactly one Spotify session

**Token store (`internal/auth/store/`)**:
- SQLite store: Store, Load, Delete operations with a real SQLite database
- Persistence: tokens survive process restart (write, stop, start, read)
- TTL cleanup: expired registrations are removed
- Concurrent access: multiple goroutines reading/writing different client_ids

**MCP server (`cmd/server/`)**:
- Server starts and prints callback URL and setup instructions
- Server fails fast with clear error if `SPOTIFY_CLIENT_ID` or `SPOTIFY_CLIENT_SECRET` are missing
- `.env` file is read, with environment variables taking precedence
- MCP tool listing returns all generated tools with correct names and descriptions
- MCP tool invocation dispatches to the correct Spotify API endpoint

**Code generator (`cmd/codegen/`, `internal/codegen/`)**:
- Fetches and parses a real OpenAPI spec (can use a local fixture copy of Spotify's spec)
- Filters out deprecated endpoints correctly
- Generated oapi-codegen client compiles and has the expected methods
- Generated MCP tool definitions compile and register correctly
- Tool names, descriptions, and parameters match the source OpenAPI spec
- Scopes are correctly extracted from the spec's security definitions

### Unit Tests

For pure logic that doesn't need external dependencies:

- PKCE code verifier/challenge generation and validation
- MCP token generation and validation
- OpenAPI spec parser: deprecated filtering logic, operationId extraction, parameter mapping
- Token expiry checking logic

### Test Infrastructure

- Use `net/http/httptest` for HTTP integration tests (test server for the MCP server, mock server for Spotify's OAuth endpoints)
- Use a temporary SQLite database per test (in-memory or temp file)
- Use `testify` for assertions (already a transitive dependency via mcp-go)
- `make test` runs all tests
- CI runs tests on every PR

## Versioning and Releases

CalVer `YYYY.MM.PATCH` (e.g., `2026.04.0`). Tags use a `v` prefix: `git tag v2026.04.0 && git push --tags`. The release workflow triggers on tag push and produces:

- Cross-platform binaries (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
- Container image pushed to `ghcr.io/makesometh-ing/spotify-mcp-go` via `ko`
- GitHub Release with all binaries and auto-generated notes
- Homebrew formula update at `makesometh-ing/homebrew-tap`

## Deployment

### Spotify App Setup (Prerequisite)

Users must register a Spotify app at https://developer.spotify.com/dashboard and configure:

1. A **Redirect URI** pointing to the MCP server's callback endpoint (e.g., `http://127.0.0.1:8080/callback`)
2. Copy the **Client ID** and **Client Secret** from the app dashboard

The server prints the exact callback URL on startup so the user knows what to register.

### Homebrew (macOS)

```bash
brew install makesometh-ing/tap/spotify-mcp-go
```

### Local binary

```bash
# Option 1: .env file
cp .env.example .env
# Edit .env with your Spotify credentials
make build
./bin/spotify-mcp-go

# Option 2: environment variables
export SPOTIFY_CLIENT_ID=your_client_id
export SPOTIFY_CLIENT_SECRET=your_client_secret
make build
./bin/spotify-mcp-go
```

### Container (ko)

```bash
make docker  # builds with ko
# Run with .env file or pass env vars to the container
```

### MCP Client Configuration

Example for Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "spotify": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

The MCP client discovers auth requirements automatically via the well-known endpoints on the same origin and handles the browser-based Spotify login.
