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

## Development Setup

### Starting Development Environment

```bash
# Start all services (Postgres, API with hot reload, Vite dev server, Nginx)
docker compose up -d arrflix

# Access the app
# - Main app: http://localhost:8484
# - Prowlarr (bundled indexer): http://localhost:9697
```

**Environment Variables**: Create `.env` file with at minimum:

```
TMDB_API_KEY=your_api_key_here
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

**Key Commands:**

```bash
cd backend

# Run API with hot reload (using Air)
air

# Build API binary
go build -o /tmp/main cmd/api/main.go

# Run tests
go test ./...

# Database: Generate SQLC code after modifying queries or schema
# (Do this after adding/modifying files in internal/db/queries/ or internal/db/migrations/)
# Use MCP tool: arrflix_sqlc_generate
sqlc generate

# OpenAPI: Regenerate the spec after humachi handler changes
# Use MCP tool: arrflix_gen_api (runs both genspec AND openapi-ts)
go run ./cmd/genspec   # writes internal/http/docs/openapi.{json,yaml}
```

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

**Key Commands:**

```bash
cd web

# Start dev server (standalone, not in Docker)
npm run dev

# Build for production
npm run build

# Type checking
npm run type-check

# Linting
npm run lint

# Code formatting
npm run format

# Regenerate API client after backend OpenAPI changes
# Use MCP tool: arrflix_gen_api (regenerates both spec and client)
npm run openapi-ts
```

**API Client**: The frontend uses auto-generated TypeScript client with TanStack Query integration. Located in `web/src/client/`, generated from `backend/internal/http/docs/openapi.json`.

### Full API Spec & Client Regeneration

When you modify backend API handlers, use the MCP tool `arrflix_gen_api` to regenerate both the OpenAPI spec and TypeScript client in one step.

Manual equivalent:
```bash
# 1. Regenerate the OpenAPI spec from humachi operation declarations
cd backend && go run ./cmd/genspec

# 2. Regenerate the TypeScript client from the spec
cd ../web && npm run openapi-ts
```

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

The project includes a custom MCP server in `mcp/` for development and operations tooling. **Use these tools instead of manual commands when available:**

| Tool | Purpose |
|------|---------|
| `arrflix_sqlc_generate` | Regenerate Go database code after modifying SQL queries or migrations |
| `arrflix_gen_api` | Regenerate OpenAPI spec AND TypeScript client after handler changes |
| `arrflix_db_query` | Run read-only SQL queries against the database |
| `arrflix_docker_logs` | Get recent logs from a docker compose service |
| `arrflix_search_repo` | Search the codebase using ripgrep |
| `arrflix_project_brief` | Get a high-level overview of the project |

## Testing

```bash
# Backend tests
cd backend && go test ./...

# Frontend tests (if configured)
cd web && npm test

# Quality testing utility
cd backend && go run cmd/quality-test/main.go
```

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
