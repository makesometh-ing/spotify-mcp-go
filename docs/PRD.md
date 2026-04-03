# Spotify MCP Server in Go - PRD

## Overview

A Model Context Protocol (MCP) server that exposes Spotify's Web API as MCP tools. Built in Go using the mcp-go library. The server implements the MCP OAuth spec as a proxy to Spotify, so MCP clients (Claude Desktop, Claude Code, Codex, Cursor) handle the browser-based login flow automatically.

A companion code generator fetches Spotify's OpenAPI spec and generates the Go source files for both the Spotify API client and the MCP tool definitions. This runs in CI on a weekly cron to keep the tool surface area current.

## System Components

Three artifacts from one repo:

1. **`spotify-mcp-go`** (cmd/server) - The MCP server. Streamable HTTP transport only. Handles MCP protocol, OAuth proxy to Spotify, tool dispatch. Distributed to users.

2. **`codegen`** (cmd/codegen) - Internal maintainer tool. Fetches the OpenAPI spec, generates Go source files (Spotify API client + MCP tool definitions). Run in CI, output committed to the repo. Not distributed.

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

**Codegen (CI only):**

| Variable | Required | Default | Description |
|---|---|---|---|
| `SPOTIFY_OPENAPI_SPEC_URL` | No | `https://developer.spotify.com/reference/web-api/open-api-schema.yaml` | OpenAPI spec URL |

## OAuth Proxy Architecture

The MCP server implements the MCP OAuth spec (RFC 8414, RFC 9728, RFC 7591) as a proxy to Spotify. MCP clients never interact with Spotify directly. The server uses PKCE (S256) on both layers: MCP client to server, and server to Spotify.

**The MCP server is responsible for Spotify token lifecycle.** MCP clients manage their own MCP tokens (issued by our server). The server transparently refreshes expired Spotify tokens when handling MCP requests. The client has no awareness of Spotify token state.

### Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/mcp` | POST | MCP streamable HTTP endpoint. Returns 401 if unauthenticated. |
| `/.well-known/oauth-protected-resource` | GET | RFC 9728 Protected Resource Metadata. Points to self as authorization server. |
| `/.well-known/oauth-authorization-server` | GET | RFC 8414 Authorization Server Metadata. Advertises authorize/token/register endpoints. PKCE required (S256). |
| `/register` | POST | RFC 7591 Dynamic Client Registration. Issues a unique client_id per MCP client. |
| `/authorize` | GET | Redirects user to Spotify's authorize endpoint with server's Spotify credentials. |
| `/callback` | GET | Receives Spotify's OAuth callback after user login. |
| `/token` | POST | Proxies token exchange and refresh to Spotify. Issues MCP tokens to clients. |

### Authorization Flow

1. MCP client hits `POST /mcp`, gets 401.
2. Client discovers auth via `GET /.well-known/oauth-protected-resource` and `GET /.well-known/oauth-authorization-server`.
3. Client registers via `POST /register`, receives a unique `client_id`.
4. Client sends user to `GET /authorize?redirect_uri=...&code_challenge=...&client_id=...`.
5. Server redirects to `https://accounts.spotify.com/authorize` (per Spotify's official docs for both standard and PKCE flows) with its own `SPOTIFY_CLIENT_ID`, a server-side callback URI (`/callback`), PKCE code_challenge (S256), and stores the client's PKCE state and redirect_uri.
6. User logs in at Spotify, grants permissions.
7. Spotify redirects to `GET /callback?code=...`.
8. Server exchanges the Spotify auth code for Spotify tokens (access + refresh) via `https://accounts.spotify.com/api/token`.
9. Server stores Spotify tokens in the token store, keyed by the MCP client_id.
10. Server generates its own MCP auth code, redirects to the client's `redirect_uri`.
11. Client exchanges the MCP auth code at `POST /token`, server issues MCP access + refresh tokens.
12. Client uses MCP access token as `Authorization: Bearer` on all `POST /mcp` requests.
13. On each MCP request, server looks up associated Spotify tokens by client_id, refreshes transparently if expired, calls Spotify API.
14. When MCP access token expires, client refreshes via `POST /token` with `grant_type=refresh_token`.

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
}
```

Ships with two implementations:

- **`SQLiteTokenStore`** (default) - Persists to `~/.config/spotify-mcp-go/auth/tokens.db`. Survives restarts.
- **`InMemoryTokenStore`** - For development/testing. Tokens lost on restart.

## Code Generator

The codegen tool (`cmd/codegen/main.go`) is an internal maintainer tool, not distributed to users.

### What It Does

1. Fetches the OpenAPI spec YAML from `$SPOTIFY_OPENAPI_SPEC_URL`.
2. Filters the spec to **active (non-deprecated) endpoints only** (currently 48 of 96).
3. Runs a two-step generation pipeline:

**Step 1: Spotify API client via oapi-codegen** (off-the-shelf, `github.com/oapi-codegen/oapi-codegen` v2.6.0):
- Generates typed Go client + request/response types from the filtered OpenAPI spec
- One method per active endpoint (e.g., `GetPlaylist(ctx, id) (*Playlist, error)`)
- Uses `net/http` under the hood, accepts auth injection via `http.Client` transport
- Configured via oapi-codegen YAML config checked into the repo

**Step 2: MCP tool definitions via custom tool** (`cmd/codegen`):
- Reads the same OpenAPI spec and generates MCP tool wiring that calls the oapi-codegen'd client
- One `mcp.Tool` per active endpoint (48 tools)
- Tool names derived from operationId (e.g., `get-playlist`, `search-tracks`)
- Descriptions from OpenAPI summary/description fields
- Parameters mapped from OpenAPI path/query/body params to mcp-go tool properties
- One handler function per tool that validates args, calls the Spotify client, returns the result

### CI Workflow

1. GitHub Action runs weekly on a cron schedule.
2. Fetches spec, runs codegen.
3. If generated files differ from committed files, opens a PR with the changes.
4. Maintainer reviews and merges.
5. Merge triggers a release (new binary + container image).

## Spotify API Coverage

Based on the current OpenAPI spec, 48 active endpoints across these categories:

| Category | Active Endpoints |
|---|---|
| Player | 15 |
| Library | 13 |
| Playlists | 10 |
| Tracks | 8 |
| Albums | 4 |
| Artists | 3 |
| Shows | 3 |
| Episodes | 3 |
| Audiobooks | 3 |
| Users | 3 |
| Chapters | 2 |
| Search | 1 |

Each active endpoint becomes one MCP tool. Tool parameters are derived from the OpenAPI spec (path params, query params, request body fields).

### Spotify OAuth Scopes

On initial authorization, the server requests all 19 Spotify API scopes upfront. This ensures any tool can be called without requiring re-authorization. Spotify supports these scopes:

| Scope | Description |
|---|---|
| `app-remote-control` | Communicate with the Spotify app on your device |
| `streaming` | Play content and control playback |
| `ugc-image-upload` | Upload images to Spotify |
| `playlist-read-private` | Access private playlists |
| `playlist-read-collaborative` | Access collaborative playlists |
| `playlist-modify-public` | Manage public playlists |
| `playlist-modify-private` | Manage private playlists |
| `user-library-read` | Access saved content |
| `user-library-modify` | Manage saved content |
| `user-read-private` | Access subscription details |
| `user-read-email` | Get email address |
| `user-follow-read` | Access followers |
| `user-follow-modify` | Manage follows |
| `user-top-read` | Read top artists and content |
| `user-read-playback-position` | Read playback position |
| `user-read-playback-state` | Read current playback and devices |
| `user-read-recently-played` | Access recently played items |
| `user-read-currently-playing` | Read currently playing content |
| `user-modify-playback-state` | Control playback |

## Project Structure

```
spotify-mcp-go/
├── .ko.yaml                     # ko build config
├── oapi-codegen.yaml            # oapi-codegen config for Spotify client generation
├── cmd/
│   ├── server/                  # MCP server entrypoint
│   │   └── main.go
│   └── codegen/                 # Internal codegen tool
│       └── main.go
├── internal/
│   ├── auth/                    # OAuth proxy layer
│   │   ├── handler.go           # Well-known, /authorize, /token, /register, /callback
│   │   ├── spotify.go           # Spotify OAuth client (code exchange, refresh)
│   │   ├── tokens.go            # MCP token issuance, validation
│   │   └── store/               # TokenStore interface + implementations
│   │       ├── store.go         # Interface definition
│   │       ├── sqlite.go        # SQLite implementation (default)
│   │       └── memory.go        # In-memory implementation
│   ├── spotify/                 # Spotify API client
│   │   ├── client.go            # Hand-written base client (http, auth injection)
│   │   ├── generated_client.go  # Generated methods
│   │   └── generated_types.go   # Generated request/response types
│   ├── tools/                   # MCP tool definitions
│   │   ├── registry.go          # Hand-written tool registration
│   │   └── generated_tools.go   # Generated tool defs + handlers
│   └── codegen/                 # MCP tool generator logic
│       ├── parser.go            # OpenAPI spec parser + deprecated endpoint filter
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
| `golang.org/x/oauth2` | v0.36.0 | Spotify OAuth client (PKCE, token exchange, refresh) |
| `modernc.org/sqlite` | latest | Pure-Go SQLite driver (no CGO, simpler cross-compilation + ko) |
| Go stdlib `net/http` | - | HTTP server, routing (ServeMux with method routing, Go 1.22+) |
| Go stdlib `crypto` | - | MCP token generation, PKCE verification |

**Dev/CI only:**

| Package | Purpose |
|---|---|
| `github.com/oapi-codegen/oapi-codegen` v2.6.0 | Generate Go client + types from OpenAPI spec |
| `gopkg.in/yaml.v3` | Parse OpenAPI YAML spec in MCP tool generator |

**Go version:** 1.26 in `go.mod`

## Deployment

### Local binary

```bash
export SPOTIFY_CLIENT_ID=your_client_id
export SPOTIFY_CLIENT_SECRET=your_client_secret
make build
./bin/spotify-mcp-go
```

### Container (ko)

```bash
export SPOTIFY_CLIENT_ID=your_client_id
export SPOTIFY_CLIENT_SECRET=your_client_secret
make docker  # builds with ko
```

### MCP client configuration

Example for Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "spotify": {
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

The MCP client discovers auth requirements automatically via the well-known endpoints and handles the browser-based Spotify login.

### Spotify App Setup

Users must register a Spotify app at https://developer.spotify.com/dashboard and configure:

1. A **Redirect URI** pointing to the MCP server's callback endpoint (e.g., `http://localhost:8080/callback`)
2. The required API scopes for the endpoints they want to use

The MCP server prints setup instructions on startup if the redirect URI has not been verified, including the exact callback URL that needs to be registered in the Spotify app.
