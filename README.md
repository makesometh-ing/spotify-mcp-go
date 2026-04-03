<p align="center">
  <img src="docs/app_logo.png" alt="Spotify MCP Go" width="180" />
</p>

<h1 align="center">spotify-mcp-go</h1>

<p align="center">
  A Model Context Protocol (MCP) server that gives AI assistants access to the Spotify Web API.
  Built in Go. OAuth handled for you. Every Spotify endpoint available as an MCP tool.
</p>

<p align="center">
  <a href="#installation">Install</a> &middot;
  <a href="#setup">Setup</a> &middot;
  <a href="#client-configuration">Connect your client</a> &middot;
  <a href="#usage-ideas">Ideas</a>
</p>

---

## What is this?

This server acts as a bridge between MCP-compatible AI assistants (Claude, Codex, Cursor, etc.) and Spotify's Web API. It implements the MCP OAuth specification as a proxy to Spotify, so your AI client handles the browser login automatically.

A code generator fetches Spotify's OpenAPI spec and generates both the Go API client and the MCP tool definitions. This runs weekly in CI to keep the tool surface current with Spotify's API.

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

Create a `.env` file in your working directory:

```bash
cp .env.example .env
```

Edit it with your Spotify credentials:

```
SPOTIFY_CLIENT_ID=your_client_id
SPOTIFY_CLIENT_SECRET=your_client_secret
```

### 3. Start the server

```bash
spotify-mcp-go
```

The server prints the MCP endpoint URL and the callback URL to register in your Spotify app.

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `SPOTIFY_CLIENT_ID` | Yes | | Spotify app client ID |
| `SPOTIFY_CLIENT_SECRET` | Yes | | Spotify app client secret |
| `SPOTIFY_MCP_PORT` | No | `8080` | HTTP server port |
| `SPOTIFY_MCP_TOKEN_DB` | No | `~/.config/spotify-mcp-go/auth/tokens.db` | SQLite token storage path |

## Client configuration

All clients connect via the MCP endpoint URL. The server handles OAuth automatically through the browser when the client first connects.

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

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

Or add to `.claude/settings.json`:

```json
{
  "mcpServers": {
    "spotify": {
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

### Cursor

Open Settings > MCP Servers > Add Server:

- **Name:** `spotify`
- **URL:** `http://localhost:8080/mcp`

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

### Any MCP client

Point your client at `http://localhost:8080/mcp`. The server advertises its OAuth endpoints via standard MCP discovery (RFC 8414, RFC 9728), so any spec-compliant client will handle auth automatically.

## Usage ideas

Once connected, your AI assistant has access to every Spotify endpoint. Here are some things you can ask:

**Music discovery**
- "Find me playlists similar to my Discover Weekly"
- "What are the audio features of my top 10 tracks? Am I in a danceable mood?"
- "Search for jazz albums released this month"

**Playlist management**
- "Create a playlist called 'Focus Mode' with 20 instrumental tracks from my library"
- "Merge my 'Running' and 'Gym' playlists, remove duplicates"
- "Find tracks that appear in more than 3 of my playlists"

**Playback control**
- "What's currently playing? Add it to my favorites"
- "Skip this track and play something by the same artist"
- "Set volume to 30% and enable shuffle"

**Analysis and reporting**
- "Show me my listening patterns this week"
- "Which artists do I listen to the most across all my playlists?"
- "Compare the tempo and energy of my morning vs evening playlists"

**Social**
- "Check what new releases dropped from artists I follow"
- "Find the most popular track from each of my followed artists"

## Versioning

This project uses [CalVer](https://calver.org/) with the format `YYYY.MM.PATCH` (e.g., `2026.04.0`). The year and month reflect when the Spotify API spec was current, and the patch number covers hotfixes within the same period.

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
