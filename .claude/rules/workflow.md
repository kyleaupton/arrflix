# Development workflow

## Task runner

The repo has a `justfile` at the root with all common dev tasks. Recipes execute inside the dev container so the environment matches CI. **Prefer these over running tools directly** — they handle the container-exec wrapping, working directory, and tool versioning.

```bash
just                 # list all recipes, grouped by area
just up              # start the dev container (Postgres, API hot-reload, Vite, Nginx via s6)
just check           # mirror CI: lint + type-check + unit tests (read-only, fast)
just check-all       # check + integration tests (slow — spins up postgres testcontainer)
just fix             # autofix everything: fmt, lint --fix, regen
just preflight       # fix then check — single command before declaring work done
```

Per-tool recipes are namespaced: `backend-fmt`, `backend-lint`, `backend-test`, `backend-test-integration`, `backend-genspec`, `backend-sqlc`, `web-fmt`, `web-lint`, `web-type-check`, `web-build`, `web-genclient`.

## Agent workflow

When work is done, run `just preflight`. If it's green, changes are ready for human review. **The user commits selectively — agents do not commit.** If `just preflight` is too slow during iteration, `just check` is the fast read-only pass.

## Codegen

When you modify backend API handlers, regenerate both the OpenAPI spec and the TypeScript client. **Order matters** — the TS client reads the spec produced by genspec:

```bash
just gen   # sqlc generate → go run ./cmd/genspec → npm run openapi-ts, in order
```

Per-step recipes (`just backend-sqlc`, `just backend-genspec`, `just web-genclient`) are available for one-off regeneration. Generated code (`backend/internal/db/sqlc/`, `web/src/client/`) is never hand-edited.

## Database migrations

Migrations run automatically on API startup via `db.ApplyMigrations()`. Add new migrations as sequentially numbered files in `backend/internal/db/migrations/`.

## Database reset & seeding

```bash
just db-reseed   # drop+recreate the arrflix schema, re-migrate, load the dev seed
```

`db-reseed` wipes the `arrflix` DB (Prowlarr's DBs are untouched), re-applies migrations via `cmd/migrate`, then loads `backend/internal/db/seed/seed_dev.sql` (dev user, libraries, name templates, downloader). The seeded downloader password comes from `DOWNLOADER_PASSWORD` in `.env` (kept out of git), defaulting to `admin`. If pgx hits a transient "cached plan" error afterward, restart the backend so its pool reconnects.

## Testing

```bash
just check                      # fast: lint + type-check + unit tests (mirrors CI per-PR)
just check-all                  # slow: above + integration tests (postgres testcontainer)
just backend-test               # unit only
just backend-test-integration   # integration only
just parity-regen               # parser parity — regen goldens from pinned Sonarr/Radarr containers
```

Unit tests live alongside the code they cover and run without external dependencies. Integration tests live in `backend/internal/test/integration/` behind a `//go:build integration` tag and spin up a postgres testcontainer per `TestMain`. The parsing module is regression-gated against Sonarr/Radarr via a hermetic Tier-1 `go test` (runs in `just check`); `just parity-regen` is the Tier-2 golden regeneration.

## Environment & access

Create a `.env` with at minimum `MEDIA_LIBRARIES=/path/to/your/media`. After `just up`:

- Main app: http://localhost:8484
- Prowlarr (bundled indexer): http://localhost:9697

## MCP tooling

A custom MCP server in `mcp/` complements the justfile for runtime introspection:

| Tool | Purpose | Prefer over |
| --- | --- | --- |
| `arrflix_db_query` | Read-only SQL against the running dev DB | shelling in + `psql` |
| `arrflix_docker_logs` | Recent logs from a docker compose service | `docker compose logs …` |

## Password hash utility

```bash
cd backend && go run cmd/password/main.go   # generate a password hash for user creation
```
