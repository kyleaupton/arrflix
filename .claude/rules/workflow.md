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
just preflight-all   # preflight plus the integration suite (the factory's done gate)
```

Inside the factory sandbox (`ARRFLIX_IN_CONTAINER=1`, see `factory/WORKFLOW.md`), the same recipes run their commands directly instead of through `docker compose exec`, and the integration tests use the sandbox's own Postgres via `ARRFLIX_TEST_PG_DSN`.

Per-tool recipes are namespaced: `backend-fmt`, `backend-lint`, `backend-vet`, `backend-test`, `backend-test-integration`, `backend-genspec`, `backend-sqlc`, `web-fmt`, `web-lint`, `web-type-check`, `web-test`, `web-build`, `web-genclient`, `gen-email`, `email-preview`. Container lifecycle: `up`, `down`, `rebuild`, `shell`, `logs`.

## Agent workflow

When work is done, run `just preflight`. If it's green, changes are ready for human review. **The user commits selectively — agents do not commit.** If `just preflight` is too slow during iteration, `just check` is the fast read-only pass.

## Codegen

When you modify backend API handlers, regenerate both the OpenAPI spec and the TypeScript client. **Order matters** — the TS client reads the spec produced by genspec:

```bash
just gen   # sqlc generate → go run ./cmd/genspec → gen-email → npm run openapi-ts
```

`gen-email` (MJML → embedded `.html.tmpl`) is independent of the API chain and runs alongside it. Per-step recipes (`just backend-sqlc`, `just backend-genspec`, `just gen-email`, `just web-genclient`) are available for one-off regeneration. Generated code (`backend/internal/db/sqlc/`, `web/src/client/`, compiled email templates) is never hand-edited; edit the `.mjml` source instead.

## Database migrations

Migrations run automatically on API startup via `db.ApplyMigrations()`. Add new migrations as sequentially numbered `NNNN_<name>.up.sql` files in `backend/internal/db/migrations/`.

## Database reset & seeding

```bash
just db-reseed   # drop+recreate the arrflix schema, re-migrate, load the dev seed
```

`db-reseed` wipes the `arrflix` DB (Prowlarr's DBs are untouched), re-applies migrations via `cmd/migrate`, then loads `backend/internal/db/seed/seed_dev.sql` (dev user, libraries, name templates, downloader). The seeded downloader password comes from `DOWNLOADER_PASSWORD` in `.env` (kept out of git), defaulting to `admin`; the TMDB key is seeded from `TMDB_API_KEY` when set. It finishes by restarting the backend, which re-seeds the default quality profiles (`QualityProfileService.SeedDefaults` runs on startup, not from SQL).

## Testing

```bash
just check                      # fast: lint + type-check + unit tests (mirrors CI per-PR)
just check-all                  # slow: above + integration tests (postgres testcontainer)
just backend-test               # unit only
just backend-test-integration   # integration only
just web-test                   # frontend component tests only
just parity-regen               # parser parity — regen goldens from pinned Sonarr/Radarr containers
```

Unit tests live alongside the code they cover and run without external dependencies. Integration tests live in `backend/internal/test/integration/` behind a `//go:build integration` tag and spin up a postgres testcontainer per `TestMain`. The parsing module is regression-gated against Sonarr/Radarr via a hermetic Tier-1 `go test` (runs in `just check`); `just parity-regen` is the Tier-2 golden regeneration.

Frontend tests are component tests: Vitest in jsdom with `@testing-library/vue`, as `*.test.ts` next to the component. API calls are mocked at the network with MSW, using the handlers generated into `web/src/client/msw.gen.ts` (`handleTitleStatusGet({ body })`, …), so components, TanStack Query and the SDK run as in the app. Render through `renderWithApp` and build payloads with the builders in `web/src/test/`. A request with no declared handler fails the test. A requirement that spans both sides gets a Go test for what the API says and a web test for what the UI does with it; a web REQ test's title starts with the ID, e.g. `test('REQ-CANCEL-009: …')`.

## Environment & access

Create a `.env` with at minimum `MEDIA_LIBRARIES=/path/to/your/media`. After `just up`:

- Main app: http://localhost:8484
- Prowlarr (bundled indexer): listens on 9696 inside the container and is not published to the host by default (uncomment its line in `docker-compose.yml` to debug it)

### Running a second dev stack (e.g. per git worktree)

Stacks are scoped by compose project name, which defaults to the checkout's directory name — distinct worktree directories already get their own Postgres volume and network. What still collides host-wide is overridable in that checkout's `.env`:

```env
COMPOSE_PROJECT_NAME=arrflix-feat-x   # only needed if the directory name clashes with another stack
ARRFLIX_DEV_CONTAINER=arrflix-dev-feat-x  # default arrflix-dev
ARRFLIX_DEV_PORT=8494                     # default 8484
ARRFLIX_DEV_IMAGE=arrflix:dev-feat-x      # default arrflix:dev; set when the worktree changes Dockerfile.dev
```

`just` recipes and the MCP server use `docker compose exec` by service name (`arrflix-dev`, fixed), so they target whichever stack belongs to the checkout they run from. `MEDIA_LIBRARIES` is a host bind mount and is shared unless each `.env` points elsewhere.

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
