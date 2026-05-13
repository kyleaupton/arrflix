# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Arrflix is a self-hosted media management platform that unifies the best parts of Sonarr, Radarr, and Overseerr into a single tool. It manages movie and series collections with a focus on filesystem integrity and efficient storage using a hardlink-first strategy.

**Tech Stack:**

- **Backend**: Go (chi router + huma/humachi for the typed API surface, PostgreSQL with pgx/v5, SQLC for type-safe queries)
- **Frontend**: Vue 3 + TypeScript (Vite, Vue Router, Pinia, TanStack Query)
- **Database**: PostgreSQL
- **API**: RESTful with OpenAPI 3.1 spec generated from huma operation declarations; typed errors emitted as RFC 9457 problem-details
- **Deployment**: Docker with s6-overlay process manager

## Task Runner

The repo has a `justfile` at the root with all common dev tasks. Recipes execute inside the dev container so the environment matches CI. **Prefer these over running tools directly** — they handle the container-exec wrapping, working directory, and tool versioning.

```bash
just                 # list all recipes, grouped by area
just up              # start the dev container
just check           # mirror CI: lint + type-check + unit tests (read-only, fast)
just check-all       # check + integration tests (slow — spins up postgres testcontainer)
just fix             # autofix everything: fmt, lint --fix, regen
just preflight       # fix then check — single command before declaring work done
```

Per-tool recipes are namespaced: `backend-fmt`, `backend-lint`, `backend-test`, `backend-genspec`, `backend-sqlc`, `web-fmt`, `web-lint`, `web-type-check`, `web-build`, `web-genclient`. The aggregate `just gen` regenerates everything (sqlc → openapi spec → ts client) in the correct order.

**Agent workflow:** when work is done, run `just preflight`. If it's green, changes are ready for human review (the user commits selectively — agents do not commit). If `just preflight` is too slow during iteration, `just check` is the fast read-only pass.

## Development Setup

### Starting Development Environment

```bash
# Start the dev container (Postgres, API w/ hot reload, Vite dev server, Nginx via s6-overlay)
just up
# or directly:
docker compose up -d

# Access the app
# - Main app: http://localhost:8484
# - Prowlarr (bundled indexer): http://localhost:9697
```

**Environment Variables**: Create `.env` file with at minimum:

```
MEDIA_LIBRARIES=/path/to/your/media
```

### Backend Development

**Project Structure:**

- `backend/cmd/api/main.go` - Main API entry point
- `backend/internal/service/` - Business logic layer (12+ services: Auth, Media, Download, Import, etc.)
- `backend/internal/repo/` - Data access layer (wraps SQLC-generated code)
- `backend/internal/http/handlers/` - HTTP request handlers
- `backend/internal/db/` - Database migrations, SQLC queries and generated code
  - `migrations/` - SQL migration files
  - `queries/` - SQLC query definitions
  - `sqlc/` - Generated type-safe Go code (do not edit manually)
- `backend/internal/downloader/` - Downloader integrations (qBittorrent, etc.)
- `backend/internal/jobs/` - Background workers (e.g., download job polling)

**Key Commands:** prefer the `just backend-*` recipes (run in container, match CI tool versions). Underlying commands shown for reference.

| Task                    | Just recipe                          | Direct equivalent                                                               |
| ----------------------- | ------------------------------------ | ------------------------------------------------------------------------------- |
| Hot-reload API          | (always running via s6 in container) | `cd backend && air`                                                             |
| Unit tests              | `just backend-test`                  | `cd backend && go test -race ./...`                                             |
| Integration tests       | `just backend-test-integration`      | `cd backend && go test -race -tags=integration ./internal/test/integration/...` |
| Format                  | `just backend-fmt`                   | `cd backend && gofmt -w .`                                                      |
| Lint (autofix)          | `just backend-lint`                  | `cd backend && golangci-lint run --fix`                                         |
| Vet                     | `just backend-vet`                   | `cd backend && go vet ./...`                                                    |
| Regenerate sqlc         | `just backend-sqlc`                  | `cd backend && sqlc generate`                                                   |
| Regenerate OpenAPI spec | `just backend-genspec`               | `cd backend && go run ./cmd/genspec`                                            |

**Database Migrations**: Migrations run automatically on API startup via `db.ApplyMigrations()`. Add new migrations as sequentially numbered files in `backend/internal/db/migrations/`.

### Frontend Development

**Project Structure:**

- `web/src/views/` - Route components (Home, Library, Settings, Movie, Series, etc.)
- `web/src/components/` - Reusable UI components organized by domain:
  - `ui/` - Base UI components (shadcn-vue style)
  - `media/`, `poster/`, `rails/` - Media display components
  - `download-candidates/` - Download selection UI
  - `settings/` - Settings pages components
- `web/src/stores/` - Pinia state management
- `web/src/client/` - Auto-generated API client (from OpenAPI spec)
- `web/src/router/` - Vue Router configuration with auth guards

**Key Commands:** prefer the `just web-*` recipes (run in container against the Linux-native `node_modules` anon volume).

| Task                  | Just recipe                          | Direct equivalent              |
| --------------------- | ------------------------------------ | ------------------------------ |
| Vite dev server       | (always running via s6 in container) | `cd web && npm run dev`        |
| Build                 | `just web-build`                     | `cd web && npm run build`      |
| Type check            | `just web-type-check`                | `cd web && npm run type-check` |
| Lint (autofix)        | `just web-lint`                      | `cd web && npm run lint`       |
| Format                | `just web-fmt`                       | `cd web && npm run format`     |
| Regenerate API client | `just web-genclient`                 | `cd web && npm run openapi-ts` |

**API Client**: The frontend uses auto-generated TypeScript client with TanStack Query integration. Located in `web/src/client/`, generated from `backend/internal/http/docs/openapi.json`.

### Full API Spec & Client Regeneration

When you modify backend API handlers, regenerate both the OpenAPI spec and TypeScript client. Order matters — the TS client reads the spec produced by genspec.

```bash
just gen   # runs sqlc generate + go run ./cmd/genspec + npm run openapi-ts in order
```

Per-step recipes (`just backend-sqlc`, `just backend-genspec`, `just web-genclient`) are also available for one-off regeneration.

## Architecture Notes

### Service Layer Pattern

The backend uses a layered architecture:

1. **HTTP Handlers** (`internal/http/handlers/`) - Handle requests, call services
2. **Services** (`internal/service/`) - Business logic, orchestrate repos and external APIs
3. **Repository** (`internal/repo/`) - Data access, wraps SQLC-generated code
4. **Database** - PostgreSQL accessed via SQLC type-safe queries

All services are initialized in `service.New()` and injected into handlers. Key services include:

- **MediaService**: Manages media metadata, integrates with TMDB
- **DownloadCandidatesService**: Searches indexers, evaluates quality policies
- **ImportService**: Hardlinks completed downloads into library
- **ScannerService**: Scans filesystem for media

### Download Flow

1. User requests media → searches indexers via **IndexerService** (wraps Prowlarr)
2. Results filtered by **PolicyEngine** based on quality profiles
3. User selects candidate → creates **DownloadJob** via **DownloadJobsService**
4. **DownloadJobsService** background worker polls downloader status
5. On completion → **ImportService** hardlinks files to library using **NameTemplates**

### State Management

- **Frontend**: Pinia stores (auth, settings, etc.) + TanStack Query for server state
- **Backend**: In-memory SSE broker for real-time updates (download progress, scan events)
- **Authentication**: JWT tokens with auth middleware on protected routes

### MCP Integration

The project includes a custom MCP server in `mcp/` for development and operations tooling. The justfile covers the build/lint/test/regenerate workflows; MCP tools complement it for runtime introspection that `just` doesn't cover.

| Tool                  | Purpose                                              | Prefer over                   |
| --------------------- | ---------------------------------------------------- | ----------------------------- |
| `arrflix_db_query`    | Run read-only SQL queries against the running dev DB | shell into container + `psql` |
| `arrflix_docker_logs` | Get recent logs from a docker compose service        | `docker compose logs ...`     |

## Testing

```bash
# Fast: lint + type-check + unit tests (mirrors CI's per-PR checks)
just check

# Slow: above + integration tests (spins up postgres testcontainer via host docker)
just check-all

# Per-suite
just backend-test               # unit only
just backend-test-integration   # integration only

# Quality testing utility (one-off, not part of check)
cd backend && go run cmd/quality-test/main.go
```

Test layering: unit tests live alongside the code they cover (`backend/internal/service/scan_test.go`, etc.) and run without external dependencies. Integration tests live in `backend/internal/test/integration/` behind a `//go:build integration` tag — they spin up a postgres testcontainer per `TestMain` and clone a per-test database from a migrated template (`backend/internal/test/dbtest/`).

## Additional Utilities

```bash
# Generate password hash for user creation
cd backend && go run cmd/password/main.go
```

## Version and Update System

Arrflix includes a built-in version tracking and update check system:

**Build Metadata:**

- Version information is injected at Docker build time via build args
- Environment variables: `ARRFLIX_VERSION`, `ARRFLIX_COMMIT`, `ARRFLIX_BUILD_DATE`, `PROWLARR_VERSION`
- Dev builds default to version `dev` with no update checks
- Edge builds (from main branch) compare commit SHAs
- Stable releases use semantic versioning and compare against GitHub releases

**API Endpoints:**

- `GET /api/v1/version` - Returns build information and update status (cached 15 minutes)

**Implementation:**

- `backend/internal/versioninfo/` - Reads environment variables
- `backend/internal/github/` - GitHub API client
- `backend/internal/semver/` - Semantic version comparison
- `backend/internal/service/version.go` - Update check logic with caching
- `web/src/components/settings/VersionCard.vue` - UI component

**Update Logic:**

- Dev builds: Always show status "unknown"
- Edge builds: Compare commit SHA with GitHub main HEAD
- Stable releases: Compare semver with latest GitHub release, show release notes
- Prereleases: Always show status "unknown"
- GitHub API responses cached for 15 minutes using existing `api_cache` table
