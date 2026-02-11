# Tambua

Real-time chat over Tailscale. Two components:
- **tambua-server**: Hosts chat servers (categories, channels, groups, access control)
- **tambua (client)**: Connects to servers, aggregates content, serves local web UI

## Quick Start

```bash
# Build
go build ./cmd/tambua
go build ./cmd/tambua-server

# Run server (appears as tambua-server.your-tailnet.ts.net)
./tambua-server

# Run client (appears as tambua.your-tailnet.ts.net)
./tambua
```

## Project Structure

- `cmd/tambua/` - Client binary
- `cmd/tambua-server/` - Server binary
- `internal/models/` - Shared data models
- `internal/protocol/` - WebSocket JSON message types
- `internal/db/` - SQLite database layer
- `internal/auth/` - Tailscale identity extraction
- `server/` - Server-specific handlers and hub
- `client/` - Client-specific handlers and aggregator
- `web/client/` - Client UI static files
- `web/admin/` - Server admin UI static files

## Architecture

- Both apps use tsnet to join the tailnet
- Server hosts channels organized in categories
- Client connects to multiple servers via WebSocket
- Access control via allow/deny lists per channel
- SQLite for persistence on both sides
- Static files served from disk (embed TODO)

## Configuration

- State directory: `~/.config/tambua/`
- Server database: `~/.config/tambua/server.db`
- Client database: `~/.config/tambua/client.db`
- Headless auth: Set `TS_AUTHKEY` environment variable

## Development

```bash
# Run with hot reload (install air: go install github.com/cosmtrek/air@latest)
air -c .air.server.toml
air -c .air.client.toml

# Or just go run
go run ./cmd/tambua-server
go run ./cmd/tambua
```

## WebSocket Protocol

JSON messages between client and server. See `internal/protocol/messages.go`.

## Dependencies

- tailscale.com/tsnet - Tailscale as a library
- github.com/gorilla/websocket - WebSocket (BSD 2-Clause)
- github.com/mattn/go-sqlite3 - SQLite driver (requires CGO)
