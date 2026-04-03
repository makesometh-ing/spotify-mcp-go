<p align="center">
  <img src="docs/app_logo.png" alt="spotify-mcp-go" width="180" />
</p>

<h1 align="center">spotify-mcp-go</h1>

<p align="center">
  MCP server for Spotify. Connect your AI assistant to the Spotify Web API.
</p>

<p align="center">
  <a href="#installation">Install</a> &middot;
  <a href="#setup">Setup</a> &middot;
  <a href="#client-configuration">Connect your client</a> &middot;
  <a href="#usage-ideas">Ideas</a>
</p>

---

## What is this?

An MCP server that exposes the Spotify Web API as tools. It handles OAuth so your MCP client (Claude Desktop, Claude Code, Cursor, etc.) can log in via the browser and start making Spotify API calls.

A code generator reads Spotify's OpenAPI spec and produces a typed Go client and MCP tool definitions. This runs weekly in CI, so when Spotify adds or changes endpoints, the tools update automatically.

## Installation

### Homebrew (macOS)

```bash
brew install makesometh-ing/tap/spotify-mcp-go
```

### Binary download

Grab the latest release for your platform from [GitHub Releases](https://github.com/makesometh-ing/spotify-mcp-go/releases).

### Container

```bash
docker pull ghcr.io/makesometh-ing/spotify-mcp-go:latest
docker run -p 8080:8080 --env-file .env ghcr.io/makesometh-ing/spotify-mcp-go:latest
```

### From source

```bash
go install github.com/makesometh-ing/spotify-mcp-go/cmd/server@latest
```

## Setup

### 1. Create a Spotify app

1. Go to [developer.spotify.com/dashboard](https://developer.spotify.com/dashboard)
2. Click "Create App"
3. Set the **Redirect URI** to `http://localhost:8080/callback`
4. Copy your **Client ID** and **Client Secret**

### 2. Configure the server

```bash
cp .env.example .env
# Edit .env with your Spotify credentials
```

```
SPOTIFY_CLIENT_ID=your_client_id
SPOTIFY_CLIENT_SECRET=your_client_secret
```

### 3. Start the server

```bash
spotify-mcp-go
```

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `SPOTIFY_CLIENT_ID` | Yes | | Spotify app client ID |
| `SPOTIFY_CLIENT_SECRET` | Yes | | Spotify app client secret |
| `SPOTIFY_MCP_PORT` | No | `8080` | HTTP server port |
| `SPOTIFY_MCP_TOKEN_DB` | No | `~/.config/spotify-mcp-go/auth/tokens.db` | SQLite token storage path |

## Client configuration

Point your client at `http://localhost:8080/mcp`. OAuth happens automatically in the browser on first connect.

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "spotify": {
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

### Claude Code

```bash
claude mcp add spotify --transport http http://localhost:8080/mcp
```

### Cursor

Settings > MCP Servers > Add Server. Name: `spotify`, URL: `http://localhost:8080/mcp`.

### Windsurf

Add to your MCP configuration:

```json
{
  "mcpServers": {
    "spotify": {
      "serverUrl": "http://localhost:8080/mcp"
    }
  }
}
```

### Codex CLI

```bash
codex mcp add spotify http://localhost:8080/mcp
```

### Other clients

Any MCP client that supports Streamable HTTP transport will work. The server advertises auth endpoints via RFC 8414 / RFC 9728 discovery.

## Usage ideas

Some things you can ask once connected:

- "What's playing right now? Add it to my favorites."
- "Create a playlist called 'Focus Mode' with instrumental tracks from my library."
- "Search for jazz albums released this month."
- "Which artists show up in more than 3 of my playlists?"
- "Merge my Running and Gym playlists, skip duplicates."
- "What new releases dropped from artists I follow?"
- "Compare the energy of my morning vs evening playlists."
- "Set volume to 30% and turn on shuffle."

## Versioning

[CalVer](https://calver.org/): `YYYY.MM.PATCH` (e.g., `2026.04.0`).

## Development

```bash
make build     # Compile binaries
make test      # Run tests with race detector
make lint      # Run golangci-lint v2
make codegen   # Regenerate from Spotify OpenAPI spec
make run       # Build and start the server
make docker    # Build container with ko
make clean     # Remove build artifacts
```

## License

MIT
