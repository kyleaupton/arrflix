# Arrflix — overview & conventions map

Arrflix is a self-hosted media management platform that unifies the best parts of Sonarr, Radarr, and Overseerr into a single tool. It manages movie and series collections with a focus on filesystem integrity and efficient storage using a hardlink-first strategy.

This file is **always loaded**. It is the orientation for design-time work — architecting a feature, deciding where logic lives, reasoning about a flow — *before* any layer-specific file is open. The detailed cheat-sheet for each layer auto-loads when you actually edit a file in that layer (see [Layer rules](#layer-rules) for the path triggers). When you design, design to the invariants below; the per-layer rules then hold you to the specifics.

## Tech stack

- **Backend**: Go — chi router + huma/humachi for the typed API surface, PostgreSQL via pgx/v5, SQLC for type-safe queries.
- **Frontend**: Vue 3 + TypeScript — Vite, Vue Router, Pinia, TanStack Query.
- **API**: RESTful, OpenAPI 3.1 generated from huma operation declarations; typed errors as RFC 9457 problem-details.
- **Deployment**: Docker with s6-overlay.

## Repo map

| Path | What lives here |
| --- | --- |
| `backend/cmd/api/main.go` | API entry point |
| `backend/internal/http/handlers/` | HTTP handlers (humachi) — request/response shapes, validation tags |
| `backend/internal/service/` | Services — business logic, orchestration; hold `*repo.Repository` |
| `backend/internal/jobs/` | Background workers (download polling, etc.) — same error rules as services |
| `backend/internal/repo/` | Data access — wraps SQLC, owns persistence↔domain translation |
| `backend/internal/<domain>/` | Pure domain modules: `parsing`, `policy`, `metadata`, `indexer`, `importer`, `matcher`, … |
| `backend/internal/errors/` | `apperrors` — typed error kinds, the wire-error model |
| `backend/internal/db/` | Migrations, SQLC queries, generated code (`sqlc/` — do not edit) |
| `web/src/views/`, `components/`, `stores/` | Frontend routes, UI, Pinia stores |
| `web/src/client/` | Auto-generated API client — do not edit |
| `specs/patterns/errors/README.md` | Full rationale for the error model |

## Architecture & conventions map

The backend is strictly layered. Requests flow **handler → service → repo → Postgres**; types and errors flow back up the same path, getting more domain-shaped at each boundary.

### Backend layering — the load-bearing invariants

- **Handlers** (`internal/http/handlers/`) — one humachi file per entity. They read parsed inputs, call one service method with typed values (`uuid.UUID`, `model.*`), and wrap the result. They **do not** construct business errors or read state; service errors flow through and render as RFC 9457 automatically. Per-field validation (required/length/enum) lives in struct tags; stateful validation lives in the service.
- **Services** (`internal/service/`) and **workers** (`internal/jobs/`) — the orchestration layer. A `*Service` holds `*repo.Repository` directly and wires domain engines to persistence. It speaks `model.*` and `uuid.UUID` only — **never** imports `dbgen.*` or `pgtype.*`. It passes repo errors through unchanged in the common case and constructs typed errors otherwise (never `errors.New`/`fmt.Errorf`).
- **Repo** (`internal/repo/`) — the only layer that touches SQLC/`pgtype`. Every method routes DB errors through `apperrors.FromPg`, returns `model.*` (never `dbgen.*`), and owns the `dbgen → model` / `pgtype ↔ Go` translation. It knows nothing about HTTP status codes.
- **Domain modules** (`internal/<domain>/`) — the **pure** half of a feature: engines, parsers, registries, aggregators, and the domain types they return. They are stateless (or hold only in-memory state) and **MUST compile without importing `internal/repo`, `internal/db/sqlc`, or `pgtype`.** When a domain module needs persistence, the `*Service` in `internal/service/` is what holds the repo and does the `domain-record → repo params` translation — **do not** define a `FooService` inside `internal/foo/` or push a repo-shaped interface into a domain package. That inversion is the most common design-time mistake; if you're reaching for it, the service belongs in `internal/service/` instead.

### The error model (cross-cutting — relevant to every design)

Errors are typed via `internal/errors` (`apperrors`). The kind determines the wire status:

| Constructor | Kind | Status |
| --- | --- | --- |
| `apperrors.NotFoundf` | NotFound | 404 |
| `apperrors.Conflictf` | Conflict | 409 |
| `apperrors.Validation(detail, fields…)` | Validation | 422 |
| `apperrors.Forbiddenf` | Forbidden | 403 |
| `apperrors.Unauthenticatedf` | Unauthenticated | 401 |
| `apperrors.BadGatewayf` | BadGateway | 502 (upstream: TMDB/Prowlarr/qBittorrent) |
| `apperrors.Internalf` | Internal | 500 (detail hidden from wire) |

Repo converts pg errors (`FromPg`); services/workers pass them through or construct the above; handlers just return them. Never introduce sentinel errors. `specs/patterns/errors/README.md` is the source of truth.

### Frontend conventions

- Vue 3 + TS. **All API access goes through the generated SDK** (`@/client/sdk.gen` / `@/client/@tanstack/vue-query.gen`) — never a hardcoded URL.
- **Server state → TanStack Query; non-API global state → Pinia** (auth, layout, dialogs, events-connection state).
- Errors arrive as typed `ProblemDetails`; surface them via `problemMessage(err, fallback)`, never `err instanceof Error`.

### Data & codegen flow

A backend API change ripples through generated code in a fixed order: **`sqlc generate` → `go run ./cmd/genspec` (OpenAPI) → `npm run openapi-ts` (TS client)**. `just gen` runs all three in order. Generated dirs (`internal/db/sqlc/`, `web/src/client/`) are never hand-edited. Migrations apply automatically on API startup.

## Layer rules

Detailed, enforceable rules for each layer live alongside this file and auto-load when you edit a matching file:

| Rule | Loads when editing |
| --- | --- |
| `backend-repo.md` | `backend/internal/repo/**` |
| `backend-service.md` | `backend/internal/service/**`, `backend/internal/jobs/**` |
| `backend-handlers.md` | `backend/internal/http/handlers/**` |
| `backend-integration-tests.md` | `backend/internal/test/integration/**` |
| `frontend-api.md` | `web/src/**` |
| `version-system.md` | version/update code |

If you are designing without those files loaded, the invariants above are the contract to design against.
