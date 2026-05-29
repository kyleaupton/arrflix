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

7. **`*Service` types live in this package. Domain modules don't define services and don't touch the repo.**
   `internal/<domain>/` packages (parsing, policy, metadata, indexer, importer, matcher, …) provide the *pure* half of a feature: engines, registries, parsers, aggregators, and the domain types they return. They are stateless or hold only in-memory state, and they MUST compile without importing `internal/repo`, `internal/db/sqlc`, or `pgtype`. The orchestration half — the thing that holds `*repo.Repository`, wires the domain engine to persistence, and is hung off `Services` in `service.go` — is a `*Service` and lives here, even when it's a thin wrapper over one domain engine (see `DownloadCandidatesService` over `policy.Engine`). A `FooService` defined inside `internal/foo/` is the layering inverted: it forces a repo-shaped interface + adapter into the domain package to dodge the import cycle, when the service could just hold `*repo.Repository` directly from here.

8. **Default to holding `*repo.Repository` directly; add an interface seam only for a real test need, and declare it here.**
   A service's repo field is the concrete `*repo.Repository`, not a hand-rolled interface — that's the norm across this package. Introduce a repo-shaped interface seam only when a service has enough untested internal logic to justify faking the repo in a co-located unit test (the per-service design decision called out under [Tests](#fakes-for-service-method-tests-for-when-seams-land)). When you do, the interface is declared in the service file next to its consumer — never in a domain package, and never as a way to let a domain package reach persistence. Domain types the engine returns (e.g. `matcher.MatchOutcomeRecord`) stay in the domain package; the `domain-record → repo.<Method>Params` translation lives in the service, the only layer allowed to know both shapes.

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

## Tests

Unit tests in this package cover internal logic that doesn't need the full HTTP/DB stack to exercise. Today that means pure helpers, parsers, and transformation code — pick the predicate or transformer next to the production code and test it co-located. The integration suite (`internal/test/integration/`) owns the wire contract; this layer owns correctness of the code behind it. The full split is in [`../test/integration/CLAUDE.md`](../test/integration/CLAUDE.md) — read it before adding a test that would need DB rows or a real external API to set up its precondition.

Service-method tests against faked dependencies are an intended future addition but **don't currently exist in this package**. They require introducing internal interface seams in the production code (the service's repo and external-client fields typed as interfaces rather than concrete `*repo.Repository` / `*TmdbService` pointers). Adding seams is a per-service design decision, not a default — do it when there's enough untested internal logic in a service to justify the production-side change. The pattern to follow when you do is at the bottom of this section.

### Scope rule

A test belongs here if the assertion is about an internal branch — a switch arm, a map lookup, a parse helper, a transformation, an error path inside a service method. Restated from the integration CLAUDE.md's deciding question, from this side: if driving the precondition through a public endpoint would require seeding rows through a non-existent admin path or running the full import/scan pipeline just to materialize state, write the unit test. The integration suite proves the wire works end-to-end; this layer proves the logic is correct. Conflating them inflates one suite without improving coverage of what only the other can catch.

### Conventions

- **Co-located.** Test for `scan.go` lives in `scan_test.go`, same directory. No separate test packages.
- **Same package** (`package service`, not `service_test`). Unit tests need access to unexported types and helpers (e.g., the `isMediaFile`/`isExtraFile` predicates in `scan.go`) — that's the whole point. Don't export something just to test it; put the test next to the thing.
- **`t.Parallel()` as the first line of every test and every subtest.** Parallel runs surface accidental shared state. If a test genuinely needs serialization (a process-level singleton, a global counter), document why in a comment above the missing `t.Parallel()`.
- **Hand-rolled fakes, no mocking framework.** When test doubles are needed, write a struct with `xxxFn func(...)` fields rather than pulling in `testify/mock` or generated mocks.
- **`t.TempDir()` for filesystem fixtures.** Auto-cleaned, isolated per test, parallel-safe.
- **`httptest.NewServer` for in-process HTTP sidecars.** Useful when the code under test wraps an HTTP client (TMDB, Prowlarr) and you want to exercise the marshaling alongside the logic without a container.
- **Naming.** `TestFunction` for free helpers (`TestIsMediaFile`, `TestBuildSearchKey`); `TestType_Case` for service-method tests when those land. Table tests for pure-function fan-out; named subtests (`t.Run("happy path", ...)`) for behavior cases.
- **Assert on typed errors via `apperrors.IsX` predicates.** Bind to the kind, not the string. Sentinels are banned (Rule 5) so `errors.Is(err, ErrFoo)` isn't an option anyway.

### Fakes for service-method tests (for when seams land)

No service in this package currently exposes the internal interface seams this section assumes; the guidance is here so the pattern is consistent when someone introduces them.

The shape: one fake struct per service-internal interface, with an `xxxFn func(...)` field for each method and a sensible zero-value default. Tests override only the methods they care about. Counters that test code reads while a service goroutine writes need a mutex — the race detector will trip otherwise under `t.Parallel()`.

A `newTestX(...)` helper that builds the service struct directly (not via the production constructor) is the natural companion — tests want to control which fakes are wired in and skip the rest. Co-location and same-package are what let that work.
