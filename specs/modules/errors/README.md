# Error handling

This is the spec for arrflix's typed error model. It covers both the design (why it's shaped this way) and the patterns (how to write code against it).

If you're writing service or repo code, **read the [Layer patterns](#layer-patterns) section.** That's where the load-bearing rules live.

## TL;DR

- One semantic axis: **`Kind`**. Drives HTTP status, derives retry behavior. 7 values total, enforced end-to-end (Go type, Postgres `error_kind` ENUM, SQLC type override).
- Wire format: **RFC 9457 `application/problem+json`**. Same shape huma emits natively.
- Error mapping happens at the **repo layer** via `apperrors.FromPg`. Services pass typed errors through; they only `Wrap` when they have better context (e.g., a slug instead of a UUID).
- Constructors return `*Error` and chain: `apperrors.Internalf("...").Op("Foo").NotRetryable()`. Builder methods are copy-on-modify; the receiver is never mutated.
- Worker retry decisions use `apperrors.IsRetryable(err)`. To override the kind-derived default for a specific callsite, chain `.NotRetryable()` at construction.
- `errors.New(...)` and bare `fmt.Errorf(...)` are forbidden at service and repo layer boundaries. Use the typed constructors.

## Why typed errors

Before this work, the backend had three problems:

1. **HTTP status codes were guessed by string-matching.** Handlers used `strings.Contains(err.Error(), "no invite")` to pick 403 vs 409 vs 500. Any rewording silently broke the contract.
2. **Internal error text leaked to clients.** `c.JSON(500, map[string]string{"error": err.Error()})` exposed SQL column names, file paths, and pgx internals.
3. **No shared vocabulary.** Services returned naked `error`. Handlers had no machine-readable way to distinguish "this was a NotFound" from "this was a Conflict" from "this is internal."

The typed error model fixes these by giving every error a stable `Kind`, mapping that kind to a stable HTTP status and wire-format identifier, and splitting user-facing detail from internal context.

## The model

### Kind axis

| Kind                     | HTTP | Retryable by default | When                                                                       |
| ------------------------ | ---- | -------------------- | -------------------------------------------------------------------------- |
| `KindNotFound`           | 404  | no                   | A specific resource doesn't exist.                                         |
| `KindConflict`           | 409  | no                   | State conflict — uniqueness violation, "already running", etc.             |
| `KindValidation`         | 422  | no                   | Input parsed fine but is semantically invalid. Carries field details.      |
| `KindForbidden`          | 403  | no                   | Authenticated but not allowed.                                             |
| `KindUnauthenticated`    | 401  | no                   | Missing or invalid credentials.                                            |
| `KindBadGateway`         | 502  | yes                  | Upstream dependency (TMDB, Prowlarr, qBittorrent) failed.                  |
| `KindInternal`           | 500  | yes                  | Unexpected internal failure. Prefer a more specific kind when one applies. |
| `KindUnspecified` (zero) | 500  | yes                  | The naked-error fallback. Treated as internal; detail is hidden.           |

Single axis. Retry is **derived** from kind via `apperrors.IsRetryable(err)`; it is not a parallel field on the error. This follows gRPC, Kubernetes, and Stripe conventions — every mature error model converges on this shape.

The kind taxonomy is enforced end-to-end:

- **Go**: `apperrors.Kind` is a typed string with named constants.
- **Postgres**: a native `error_kind` ENUM with the same values, used as the column type on `download_job.error_kind` and `import_task.error_kind`. The DB rejects invalid values.
- **SQLC**: a type override maps the postgres enum directly to `apperrors.Kind`, so generated queries take/return the same Go type.

Adding a new kind therefore requires three coordinated changes (Go constant, ENUM `ALTER TYPE ... ADD VALUE`, sqlc regen). That friction is the point: kinds are part of the public API contract and shouldn't be added casually.

For the rare callsite where the kind's default retry behavior is wrong — e.g., an `Internal` error that represents a programming-error / invariant violation rather than a transient failure — chain `.NotRetryable()` on the constructor. See [Layer patterns](#layer-patterns).

### Wire format

All error responses are RFC 9457 `application/problem+json`:

```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "library 7f3a-... not found"
}
```

For validation errors, an `errors` array carries field-level detail (huma's convention):

```json
{
  "type": "/errors/validation",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "invalid library",
  "errors": [
    { "location": "body.name", "message": "required" },
    { "location": "body.type", "message": "must be 'movie' or 'series'" }
  ]
}
```

The `type` field is a **stable identifier**. Frontends branch on it. It is part of the public API contract — never rename a `type` value once shipped. The path is relative; we don't have to host docs at it (we may eventually).

`detail` is user-safe by construction:

- For client errors (4xx kinds), `detail` is whatever the constructor's format string produced. The author is responsible for not putting secrets there.
- For 500s and unspecified-kind errors, `detail` is replaced with `"an internal error occurred"`. The original message stays in logs only.

`location` for validation follows huma's format: `body.field_name`, `path.id`, `query.param`, etc. This is the convention so service-emitted validation errors look identical to huma's binding-emitted ones.

## Layer patterns

The patterns below are enforced by `CLAUDE.md` files in the relevant directories. Read those before writing code; this section is the rationale.

### Repo layer: `FromPg` always

Every repo method that wraps a SQLC query routes the result through `apperrors.FromPg`:

```go
func (r *Repository) GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
    lib, err := r.Q.GetLibrary(ctx, id)
    return lib, apperrors.FromPg(err, "library %s not found", id)
}
```

`FromPg` translates:

- `pgx.ErrNoRows` → `KindNotFound`
- SQLSTATE `23505` (unique violation) → `KindConflict`
- SQLSTATE `23503` (foreign key violation) → `KindConflict`
- SQLSTATE `23502` (not-null violation) → `KindValidation`
- SQLSTATE `23514` (check violation) → `KindValidation`
- anything else → wrapped without a kind (surfaces as 500)

The format string carries the entity name and the ID the repo received. That detail is good enough for 90% of callers. When the service has a better identifier (a slug, a composite framing), it overrides via `Wrap` — see below.

This layer is also where postgres-error-code knowledge lives. Services don't know about SQLSTATE codes; they only see typed kinds.

### Service layer: pass through, `Wrap` to override

Most service methods just consume the repo's typed error unchanged:

```go
func (s *LibrariesService) Get(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
    return s.repo.GetLibrary(ctx, id)  // typed error from repo passes through
}
```

Use `apperrors.Wrap` **only** when you have better context than the repo had:

```go
// Service got a slug from the URL; repo only knows the UUID.
func (s *MediaService) GetBySlug(ctx context.Context, slug string) (dbgen.Media, error) {
    id, err := s.resolveSlug(ctx, slug)
    if err != nil {
        return dbgen.Media{}, err
    }
    m, err := s.repo.GetMedia(ctx, id)
    if apperrors.IsNotFound(err) {
        // Replace the UUID-based detail with a slug-based one.
        return m, apperrors.Wrap(err, "media %q not found", slug)
    }
    return m, err
}
```

`Wrap` replaces the user-facing detail while preserving the kind, the captured stack, and the original error in the wrap chain (so logs still see everything).

Rule of thumb:

- **Same identifier shape as input → repo handles it natively.** No service code needed.
- **Different identifier shape than what the user knows → service `Wrap`s** at the resolution point.

For input validation, use `apperrors.Validation` with field-level details:

```go
func (s *LibrariesService) Create(ctx context.Context, name, typ, rootPath string, ...) (dbgen.Library, error) {
    var fields []apperrors.FieldError
    if name == "" {
        fields = append(fields, apperrors.Field("body.name", "required"))
    }
    if typ != "movie" && typ != "series" {
        fields = append(fields, apperrors.Field("body.type", "must be 'movie' or 'series'"))
    }
    if len(fields) > 0 {
        return dbgen.Library{}, apperrors.Validation("invalid library", fields...)
    }
    return s.repo.CreateLibrary(ctx, ...)
}
```

Collect all field errors before returning so the user sees every problem at once, not one-at-a-time.

#### Chaining `.Op()` and `.NotRetryable()`

When constructing fresh typed errors, chain the builder methods rather than using the wrap-style `WithOp` / `AsPermanent` helpers:

```go
return apperrors.Internalf("unknown protocol: %s", job.Protocol).
    Op("DownloadWorker.poll").
    NotRetryable()
```

- `.Op(name)` records the operation for structured logs (visible via `slog.LogValue`, never on the wire).
- `.NotRetryable()` flips the retry decision for cases where the kind's default is wrong — typically `Internal` errors that represent invariant violations or programming errors. Use it sparingly; if multiple callsites in the same area need it, the kind taxonomy may be missing a value.

Builder methods are copy-on-modify: the receiver is never mutated, so an error reused across goroutines stays consistent.

### Handler layer: render to RFC 9457

> Not yet implemented. As of writing, handlers still use Echo and inline `c.JSON(...)` patterns. When we migrate handlers (in coordination with the huma migration), this section will be filled in. The pattern will be:
>
> ```go
> if err != nil {
>     return renderError(c, err)  // emits application/problem+json with the right status
> }
> ```
>
> Until then, existing handlers continue to function as written. New handler work should still emit typed errors from services so the eventual migration is mechanical.

### Worker layer: typed kinds + `IsRetryable`

Workers (`internal/jobs/*`) construct typed errors and route the retry decision through `apperrors.IsRetryable(err)`:

```go
func (w *Worker) handleError(ctx context.Context, job dbgen.DownloadJob, err error) {
    msg := err.Error()
    kind := apperrors.KindOf(err)

    if !apperrors.IsRetryable(err) {
        _, _ = w.repo.MarkDownloadJobFailed(ctx, job.ID, msg, kind)
        return
    }

    // ... attempt-count check, exponential backoff, ScheduleDownloadJobRetry ...
}
```

For invariant violations and programming errors that should not retry — "unknown protocol", "missing external id" — construct an `Internal` and chain `.NotRetryable()`:

```go
return apperrors.Internalf("unknown protocol: %s", job.Protocol).NotRetryable()
```

The kind written to the DB column comes from `apperrors.KindOf(err)`. Naked errors (no typed kind) are persisted as NULL.

## Key API surface

Defined in `backend/internal/errors/`.

**Constructors** — `NotFoundf`, `Conflictf`, `Forbiddenf`, `Unauthenticatedf`, `BadGatewayf`, `Internalf`, `Validation`. All return `*Error` so they can be chained.

**Builder methods on `*Error`** (copy-on-modify, never mutate the receiver):

- `.Op(name)` — record an operation name for structured logs.
- `.NotRetryable()` — mark this error as not-retryable regardless of kind. Use sparingly; prefer choosing a kind whose default is correct.

**Wrap-style helpers** (take `error`, return `error`, nil-safe, not chainable) — `Wrap` (replace detail, preserve kind), `WithOp` (record op on a non-typed error), `FromPg` (the repo-layer translation point).

**Persistence** — `error_kind` Postgres ENUM mirrors the Go `Kind` type. Mapped via SQLC override in `sqlc.yml`; column appears on `download_job` and `import_task` (nullable; NULL for naked-error fallback).

**Predicates** — `KindOf`, `IsKind`, `IsNotFound`, `IsConflict`, `IsValidation`, `IsForbidden`, `IsUnauthenticated`, `IsBadGateway`, `IsInternal`, `IsRetryable`.

**Adapters** — `FromPg` (the standard repo-layer translation point).

**Wire** — `ToProblem(err) ProblemDetails`, `HTTPStatus(err) int`, `ContentType` constant.

**Logging** — `*Error` implements `slog.LogValue`. Pass errors directly to slog and you get `kind`, `msg`, `op`, `fields`, and `stack` as structured attributes.

## What we deliberately don't have

- `RateLimited` (429) — no API rate-limiting today; YAGNI.
- `Gone` (410) — too niche; not a real signal in the codebase.
- `Unavailable` (503) — only useful at infrastructure boundaries; arrflix doesn't have those today.
- A "BadRequest" (400) kind — `Validation` (422) covers semantically invalid input; truly unparseable requests are a framework concern (huma handles them).
- A separate `code` field for granular error codes (Stripe-style) — RFC 9457 allows extension members, so we can add `code` later without breaking the contract. Don't add it speculatively.

If you find yourself wanting one of these, push back first: the cost of every kind is forever — every future contributor (human or AI) has to learn it. Every kind we have today corresponds to a real signal that exists in the code.

## Key design decisions

Captured here so we don't relitigate them.

- **Single Kind axis, not Kind × Category.** Industry standard (gRPC, Kubernetes, Stripe). Retry derives from kind. The `.NotRetryable()` builder method handles the rare override case.
- **Kind enforced end-to-end via Postgres ENUM + SQLC override.** Same vocabulary in Go, the DB, and the wire. Adding a kind requires migration + Go change + sqlc regen — that friction is intentional, since kinds are stable contract.
- **Validation = 422, not 400.** Aligns with huma's binding errors. 400 is for unparseable, 422 is for "parsed fine, semantically invalid."
- **Repo-layer Pg-error mapping.** Pg-error knowledge belongs at the storage boundary. Services consume typed errors and `Wrap` only when adding context.
- **`Wrap` replaces user-facing detail; preserves kind + chain + stack.** This is the override mechanism that lets services improve on the repo's default identifier without re-implementing translation.
- **Hide internal detail in 500s.** Prevents SQL leaks. Logs still have everything via `slog.LogValue`.
- **Hand-rolled stack traces, not `cockroachdb/errors`.** ~30 lines of `runtime.Callers`. No new dependency. Can swap in cockroachdb later if we ever want its richer features.
- **Wire `type` is a relative URI string we don't have to publish.** It's a stable identifier, not a resource locator. Frontend branches on it; docs can come later.

## Known limitations

These are intentional trade-offs documented so future readers know they're chosen, not overlooked.

- **`last_error` is stored unfiltered.** The worker persists `err.Error()` directly to `download_job.last_error` and `import_task.last_error`. For typed errors with `%w` wrapping, that string contains the original upstream error text. This is fine for arrflix today because it's self-hosted and single-tenant — the user IS the admin, and seeing their own job's error details is the intended behavior. **If multi-tenancy is ever added, revisit:** `last_error` would need to be sanitized (e.g., `KindOf(err)`-aware truncation) before persistence, or the API needs to filter the column out of public responses. The leak that `ToProblem` closes for HTTP responses is reopened through this column for any handler that returns the raw job row.

- **`IsRetryable` walks `errors.Unwrap`, not `errors.Join`.** Multi-error values produced by `errors.Join(...)` (Go 1.20+) implement `Unwrap() []error`, which `errors.Unwrap` does not traverse. Nothing in arrflix uses `errors.Join` today; if that changes, the predicate will need updating to walk multi-wrap chains.

- **`.NotRetryable()` is runtime-only, not persisted.** A failed job's row records `error_kind` and `status='failed'` but not whether the override flipped retryability. Three failure modes (permanent / retryable-but-exhausted / kind-default-no-retry) collapse to the same DB state. Today nothing queries this distinction post-failure; when something does, the principled fix is sharper kinds (e.g., a future `KindFailedPrecondition`), not a persisted boolean override.

## Migration status

- ✅ `internal/errors` package implemented (`Kind`, constructors, `Wrap`, predicates, `ProblemDetails`, `FromPg`, `slog.LogValue`).
- ✅ Postgres `error_kind` ENUM, `error_kind` columns on `download_job` and `import_task`, SQLC override mapping the enum to `apperrors.Kind`.
- ✅ Worker code (`internal/jobs/download`, `internal/jobs/import`) uses typed kinds and `IsRetryable`; the legacy `Category` API is gone.
- ⏳ Service and repo layers — not yet migrated. Existing code continues to function; new code should follow the layer patterns above.
- ⏳ Handler layer — not migrated. Pending the huma migration; handler patterns will be added to this spec when that work begins.
- ⏳ Existing service-layer sentinels (e.g., `service.ErrScanAlreadyRunning`) — to be replaced with `Conflictf` constructors over time. Not blocking.
