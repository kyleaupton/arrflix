# Service and worker layer rules

These rules apply to both service code (`internal/service/`) and worker code (`internal/jobs/`). Services orchestrate business logic; workers drive long-running jobs. Both consume typed errors from `internal/repo`, may invoke external dependencies, and return typed errors. Where a rule is service-specific or worker-specific the text calls it out; otherwise treat both layers the same.

**Read [`specs/errors/README.md`](../../../specs/errors/README.md) before writing error-producing code.** This file is the layer-specific cheat sheet; the spec has the full rationale.

## Rules

1. **Repo errors are already typed. Pass them through unchanged in the common case.**
   No `if err != nil { return apperrors.NotFound... }` after a repo call. The repo did that work. (Same rule applies to `internal/jobs/*` workers.)

2. **Use `apperrors.Wrap` ONLY when you have a better identifier than the repo had.**
   E.g., the user passed a slug; the repo only knows the UUID. `Wrap` replaces the `detail` while preserving the kind, the captured stack, and the original error chain (so logs still see everything).

3. **Never use `errors.New(...)` or `fmt.Errorf(...)` in service or worker code.**
   Those produce naked errors that surface as 500s with hidden details. Use the typed constructors: (Same rule applies to `internal/jobs/*` workers.)
   - `apperrors.NotFoundf` — 404
   - `apperrors.Conflictf` — 409 (state conflicts: "already exists", "already running")
   - `apperrors.Validation(detail, fields...)` — 422 (invalid input)
   - `apperrors.Forbiddenf` — 403
   - `apperrors.Unauthenticatedf` — 401
   - `apperrors.BadGatewayf` — 502 (upstream API failures: TMDB, Prowlarr, qBittorrent)
   - `apperrors.Internalf` — 500 (only when nothing more specific applies)

4. **For input validation, collect every field problem before returning.**
   The user should see all errors at once, not one-at-a-time. Use `apperrors.Validation` with multiple `apperrors.Field` entries.

5. **Don't introduce sentinel errors (`var ErrFoo = errors.New(...)`).**
   Sentinels were how we used to signal "this specific failure" before kinds existed. Now the kind plus the detail message carry that signal. Existing sentinels (e.g., `ErrScanAlreadyRunning`) will be migrated as we touch them; don't add new ones. (Same rule applies to `internal/jobs/*` workers.)

6. **Never import `dbgen.*` or `pgtype.*`.**
   Services and workers speak `model.*` (idiomatic Go domain types). The repo handles the persistence ↔ domain translation. Service signatures take and return `model.*` and `uuid.UUID`; pgtype-shaped values are a repo-internal concern. If a service file imports `github.com/kyleaupton/arrflix/internal/db/sqlc` or `github.com/jackc/pgx/v5/pgtype`, the layering is wrong — fix the repo, not the service. (Same rule applies to `internal/jobs/*` workers.)

## Patterns

### Pass-through (the common case)

```go
func (s *LibrariesService) Get(ctx context.Context, id uuid.UUID) (model.Library, error) {
    return s.repo.GetLibrary(ctx, id)  // typed error from repo flows through
}
```

### Override the detail when the service has better context

```go
func (s *MediaService) GetBySlug(ctx context.Context, slug string) (model.Media, error) {
    id, err := s.resolveSlug(ctx, slug)
    if err != nil {
        return model.Media{}, err
    }
    m, err := s.repo.GetMedia(ctx, id)
    if apperrors.IsNotFound(err) {
        // Repo emitted "media <uuid> not found"; the user only knows the slug.
        return m, apperrors.Wrap(err, "media %q not found", slug)
    }
    return m, err
}
```

### Input validation — collect all field errors

```go
func (s *LibrariesService) Create(ctx context.Context, name, typ, rootPath string, ...) (model.Library, error) {
    var fields []apperrors.FieldError
    if name == "" {
        fields = append(fields, apperrors.Field("body.name", "required"))
    }
    if typ != "movie" && typ != "series" {
        fields = append(fields, apperrors.Field("body.type", "must be 'movie' or 'series'"))
    }
    if rootPath == "" {
        fields = append(fields, apperrors.Field("body.root_path", "required"))
    }
    if len(fields) > 0 {
        return model.Library{}, apperrors.Validation("invalid library", fields...)
    }
    return s.repo.CreateLibrary(ctx, name, typ, rootPath, ...)
}
```

`location` strings follow huma's convention: `body.field_name`, `path.id`, `query.param`. This makes service-emitted validation errors look identical to huma's binding-emitted ones on the wire.

### Conflict for state-based rejections

```go
func (s *ScannerService) StartScan(ctx context.Context, libraryID uuid.UUID) (string, error) {
    if _, loaded := s.running.LoadOrStore(key, scanID); loaded {
        return "", apperrors.Conflictf("scan already running for library %s", libraryID)
    }
    ...
}
```

### Upstream API failure → BadGateway

```go
result, err := s.tmdb.Search(ctx, query)
if err != nil {
    return nil, apperrors.BadGatewayf("tmdb search failed: %v", err)
    // Or, if you want the original in the chain for logs:
    // return nil, apperrors.Wrap(apperrors.BadGatewayf("tmdb search failed"), err.Error())
}
```

`BadGateway` is retryable by default — workers can retry it without an `AsTransient` wrap.

### Add op context for logs

**Every freshly-constructed typed error at a service or handler boundary chains `.Op()`.** The naming convention is `<Type>.<Method>` in PascalCase (e.g., `"LibrariesService.Create"`, `"DownloadCandidatesService.Search"`, `"LibrariesHandler.Scan"`).

```go
// Service: chain .Op() on every typed-error construction.
func (s *LibrariesService) Create(ctx context.Context, name, typ, rootPath string, ...) (model.Library, error) {
    if name == "" {
        return model.Library{}, apperrors.Validation("invalid library",
            apperrors.Field("body.name", "required"),
        ).Op("LibrariesService.Create")
    }
    // ...
}

// Handler: same rule applies at the handler boundary.
return RenderError(c, apperrors.Conflictf("scan already running for library %s", id).
    Op("LibrariesHandler.Scan"))
```

When the rule does NOT apply:

- **Pass-through repo errors.** The repo's `FromPg` already produced a typed error; don't re-Op it. The op for "this came from the database layer" is implicit in the call path.
- **`apperrors.Wrap` calls.** `Wrap` is for replacing the user-facing detail with better context — if you also need an op there, use `apperrors.WithOp(apperrors.Wrap(...), "X.Y")` only when the calling context adds operational signal beyond the kind+detail. In practice, plain `Wrap` is fine; reserve `WithOp` for cases where a non-typed external error crosses the service boundary and needs op annotation.

For non-typed errors from external code that you're surfacing as-is (rare), use `WithOp`:

```go
lib, err := someExternalAPI()
return lib, apperrors.WithOp(err, "LibrariesService.Get")
```

`Op` and `WithOp` record the operation in structured logs (via `slog.LogValue`) but don't change the wire format.

### `Bind()` failures: shape rule

Echo's `c.Bind(&req)` returns an error when the JSON is unparseable, the Content-Type is wrong, or a struct tag rejects the input. The `err.Error()` text is **leaky** — it can include field paths the API doesn't otherwise document, JSON parser internals, or struct field names that drift away from the wire schema. Don't put it on the wire.

The canonical shape:

```go
if err := c.Bind(&req); err != nil {
    c.Logger().Error(err)  // keep the parse detail in logs
    return RenderError(c, apperrors.Validation("invalid request body").
        Op("LibrariesHandler.Create"))
}
```

- No `apperrors.Field("body", err.Error())` — that's the leak.
- No field details at all on the wire. The user sees "invalid request body" with status 422; ops sees the underlying parse error in the log line.
- Op uses the handler+method name so logs can be filtered by route.

### Override retry semantics for invariant violations

For `Internal` errors that should NOT retry — programming errors, invalid state, configuration bugs — chain `.NotRetryable()`:

```go
return apperrors.Internalf("unknown protocol: %s", proto).
    Op("DownloadWorker.poll").
    NotRetryable()
```

Use sparingly. The kind already encodes the default retry behavior; `.NotRetryable()` is for the rare case where Internal is the right kind for the wire but you know retrying is hopeless.

### Workers and retry semantics

Worker error sites care about retryability in addition to the kind. The defaults are derived from the kind (see [`specs/errors/README.md`](../../../specs/errors/README.md)): `apperrors.BadGateway` is retryable by default, so workers should use it for upstream API failures (qBittorrent, TMDB, Prowlarr) — the worker loop will back off and try again without any extra annotation. `apperrors.Internalf(...)` is also retryable by default, but invariant violations the worker can't recover from (unknown protocol, missing external id, "this should never happen") should chain `.NotRetryable()` so a single failure trips the job to `failed` instead of burning attempts. Repo errors flow through unchanged — `KindNotFound` and `KindConflict` are non-retryable by default, which is the right behavior for "the row vanished" or "we already enqueued this."

## What goes in `errors.New` / `fmt.Errorf` instead

Nothing. If you find yourself reaching for them in service or worker code, ask:

- **Is it user input?** → `apperrors.Validation`.
- **Is it state conflict?** → `apperrors.Conflictf`.
- **Is it a missing resource?** → `apperrors.NotFoundf` (or pass through the repo's).
- **Is it an upstream failure?** → `apperrors.BadGatewayf`.
- **Is it a programmer error / "this should never happen"?** → `apperrors.Internalf`. Logged, hidden from the wire.

If none of those fit, the answer is probably to read the spec — there might be a kind missing, or there might be a layering question to think about.
