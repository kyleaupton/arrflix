# Arrflix MCP server

Project-specific MCP tools that complement the `justfile` task runner. Build/lint/test/regenerate workflows live in the justfile; MCP is reserved for runtime introspection that benefits from structured I/O.

## Tools

| Tool | Purpose |
|---|---|
| `arrflix_db_query` | Run a READ-ONLY Postgres query (SELECT/CTE only) against `ARRFLIX_DATABASE_URL`. Returns JSON rows. Parameterized via `params`, bounded by `maxRows` (default 200, cap 500). |
| `arrflix_docker_logs` | Tail recent docker compose logs for a service. Bounded by `lines` (default 200, cap 2000). |

## Configuration

Create `mcp/.env` with at minimum:

```
ARRFLIX_DATABASE_URL=postgres://user:pass@host:port/dbname
```

The DB tool refuses to start a query if `ARRFLIX_DATABASE_URL` is unset.

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
