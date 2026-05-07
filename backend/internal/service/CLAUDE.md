# Service layer rules

Services orchestrate business logic. They consume typed errors from `internal/repo`, validate inputs, and return typed errors that handlers render to clients.

**Read [`specs/errors/README.md`](../../../specs/errors/README.md) before writing error-producing code.** This file is the layer-specific cheat sheet; the spec has the full rationale.

## Rules

1. **Repo errors are already typed. Pass them through unchanged in the common case.**
   No `if err != nil { return apperrors.NotFound... }` after a repo call. The repo did that work.

2. **Use `apperrors.Wrap` ONLY when you have a better identifier than the repo had.**
   E.g., the user passed a slug; the repo only knows the UUID. `Wrap` replaces the `detail` while preserving the kind, the captured stack, and the original error chain (so logs still see everything).

3. **Never use `errors.New(...)` or `fmt.Errorf(...)` in service code.**
   Those produce naked errors that surface as 500s with hidden details. Use the typed constructors:
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
   Sentinels were how we used to signal "this specific failure" before kinds existed. Now the kind plus the detail message carry that signal. Existing sentinels (e.g., `ErrScanAlreadyRunning`) will be migrated as we touch them; don't add new ones.

## Patterns

### Pass-through (the common case)

```go
func (s *LibrariesService) Get(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
    return s.repo.GetLibrary(ctx, id)  // typed error from repo flows through
}
```

### Override the detail when the service has better context

```go
func (s *MediaService) GetBySlug(ctx context.Context, slug string) (dbgen.Media, error) {
    id, err := s.resolveSlug(ctx, slug)
    if err != nil {
        return dbgen.Media{}, err
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
func (s *LibrariesService) Create(ctx context.Context, name, typ, rootPath string, ...) (dbgen.Library, error) {
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
        return dbgen.Library{}, apperrors.Validation("invalid library", fields...)
    }
    return s.repo.CreateLibrary(ctx, name, typ, rootPath, ...)
}
```

`location` strings follow huma's convention: `body.field_name`, `path.id`, `query.param`. This makes service-emitted validation errors look identical to huma's binding-emitted ones on the wire.

### Conflict for state-based rejections

```go
func (s *ScannerService) StartScan(ctx context.Context, libraryID pgtype.UUID) (string, error) {
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

For typed errors you're constructing, chain `.Op()`:

```go
return apperrors.NotFoundf("library %s not found", id).Op("LibrariesService.Get")
```

For non-typed errors from external code, use `WithOp` (returns `error`):

```go
lib, err := someExternalAPI()
return lib, apperrors.WithOp(err, "LibrariesService.Get")
```

`Op` and `WithOp` record the operation in structured logs (via `slog.LogValue`) but don't change the wire format.

### Override retry semantics for invariant violations

For `Internal` errors that should NOT retry — programming errors, invalid state, configuration bugs — chain `.NotRetryable()`:

```go
return apperrors.Internalf("unknown protocol: %s", proto).
    Op("DownloadWorker.poll").
    NotRetryable()
```

Use sparingly. The kind already encodes the default retry behavior; `.NotRetryable()` is for the rare case where Internal is the right kind for the wire but you know retrying is hopeless.

## What goes in `errors.New` / `fmt.Errorf` instead

Nothing. If you find yourself reaching for them in service code, ask:

- **Is it user input?** → `apperrors.Validation`.
- **Is it state conflict?** → `apperrors.Conflictf`.
- **Is it a missing resource?** → `apperrors.NotFoundf` (or pass through the repo's).
- **Is it an upstream failure?** → `apperrors.BadGatewayf`.
- **Is it a programmer error / "this should never happen"?** → `apperrors.Internalf`. Logged, hidden from the wire.

If none of those fit, the answer is probably to read the spec — there might be a kind missing, or there might be a layering question to think about.
