# Arrflix MCP server

Project-specific MCP tools that complement the `justfile` task runner. Build/lint/test/regenerate workflows live in the justfile; MCP is reserved for runtime introspection that benefits from structured I/O.

## Tools

| Tool | Purpose |
|---|---|
| `arrflix_db_query` | Run a READ-ONLY Postgres query (SELECT/CTE only) against the dev database. Returns JSON rows. Bounded by `maxRows` (default 200, cap 500). |
| `arrflix_docker_logs` | Tail recent docker compose logs for a service. Bounded by `lines` (default 200, cap 2000). |

## Configuration

No configuration is required. The DB tool runs `psql` *inside* the compose
service via `docker compose exec`, connecting over the container's local unix
socket (trust auth) — so it needs no host, port, or password, and the `5432`
line in `docker-compose.yml` stays commented out.

The defaults match the dev seed; override in `mcp/.env` only if yours differ:

```
ARRFLIX_DB_SERVICE=arrflix-dev   # compose service to exec into
ARRFLIX_DB_USER=arrflix          # psql role
ARRFLIX_DB_NAME=arrflix          # database
```

## Build & develop

```bash
npm install
npm run build      # tsc + chmod build/index.js
npm run watch      # rebuild on save
npm run inspector  # launch the MCP inspector against the built server
```

The compiled entry is `build/index.js` and is the executable referenced by client configs.

## Installing in a client

Point your MCP-aware client (Claude Code, Claude Desktop, Cursor, etc.) at the built binary:

```json
{
  "mcpServers": {
    "arrflix": {
      "command": "/absolute/path/to/arrflix/mcp/build/index.js"
    }
  }
}
```
