# Integration test rules

These tests exercise the public HTTP surface of arrflix end-to-end: real chi
router, real huma binding, real auth middleware, real services against a
real Postgres (per-test cloned database via `internal/test/dbtest`), with
external dependencies (TMDB, eventually Prowlarr/qBittorrent) faked at the
HTTP layer so requests stay in-process.

Run with `go test -tags=integration -count=1 ./internal/test/integration/...`.

## Wire-shape rule: type for responses, untyped for requests

The single most important rule for tests in this package:

- **Decode responses into the production type.** Use `model.Library`, not a
  local mirror struct. Use `apperrors.ProblemDetails` / `apperrors.FieldError`,
  not local copies.
- **Build requests as `map[string]any` (or raw JSON bytes).** Do NOT export
  the handler's huma input struct just so tests can use it. Helpers like
  `makeCreateBody` belong here.

This asymmetry is principled, not accidental. The reason matters because the
temptation to "just mirror the response struct locally for convenience" or
"just export the request type for type safety" comes up every time someone
writes a new test.

### Why response types

Response types are **the contract**. What the handler emits is what real
clients depend on. Binding tests to `model.Library` means the test enforces
that contract: any field rename, tag drift, or shape change in the
production type immediately breaks the test. That is the entire point of
writing an HTTP-level integration test.

Mirroring the response shape locally defeats that:

- **Renames pass silently.** If `model.Library.RootPath` becomes `Root` (JSON
  tag updated), the real wire format changes — frontend regenerates, clients
  break. But the local mirror still says `RootPath` with tag `"rootPath"`, so
  the test decodes a zero value, the equality check fires correctly against
  the local mirror's idea of the shape, and the test reports green while
  production is broken. The test has frozen a historical wire format.
- **New fields are invisible.** A test that wants to assert on a newly-added
  field can't, until someone updates the mirror. Tests describe a stale
  subset of the wire format.
- **Type loss is real.** `model.Library.ID` is `uuid.UUID`, not `string`.
  Successful decode IS the format validation — no separate `uuid.Parse`
  check needed. Same for `time.Time` fields (RFC3339 round-trip is free).

If `model.Library` ever splits into "internal domain model" + "API DTO"
(common when external clients freeze on a versioned wire format), tests
should bind to the DTO. Today the domain type IS the wire type, so we use it.

### Why NOT request types

Request types are **a binding mechanism**, not a contract. Huma's input
structs (`type Input struct { Body struct{...} }`) drive validation and
OpenAPI generation through struct tags (`minLength`, `enum`, `required`).
They describe one *valid* encoding of a request — but tests need to send
things outside that valid encoding to be useful:

- Empty `name` to assert `minLength:"1"` fires.
- `"type": "bogus"` to assert `enum:"movie,series"` fires.
- Missing required fields, wrong types (`"enabled": "yes"`), extra unknown
  fields, malformed JSON.

A typed Go struct can't express most of those — it would refuse to compile
or zero-value the field. `map[string]any` (or raw JSON bytes) sits on the
wire side of the binding boundary, where validation tests need to live.

Put another way:

- **Response types are an upper bound** of what the API emits — "exactly
  this shape." Test binding works.
- **Request types are a lower bound** of what the API accepts — "at least
  these shapes work." Tests must probe outside the bound.

There's a coupling reason too: exporting the input struct ties tests to a
handler implementation detail. If a handler splits its `Body` into separate
path/query/body inputs, or renames a Go field while keeping the JSON tag,
the wire contract is unchanged but type-bound tests would break. Map-based
tests stay green because they describe what's on the wire, which hasn't
changed.

The only case where this might be revisited: if arrflix ever ships a
first-party Go client SDK, that SDK would expose request types, and tests
could share them with the SDK. No such consumer exists today.

## Other conventions

- **One package**, `package integration`. All tests share a single
  `TestMain` (`main_test.go`) that boots the Postgres testcontainer once.
  Resist splitting into subpackages — each one would need its own
  container boot.
- **Per-test DB clone** via `dbtest.New(t)`. Cheap (`CREATE DATABASE …
  TEMPLATE`), fully isolated, no cross-test state.
- **Per-test app** via `testapp.New(t, pool, opts...)`. Wires the full HTTP
  stack, marks the system initialized, creates an admin user, and pre-issues
  an admin JWT.
- **Inject fakes for external clients via testapp options.** Today only
  `testapp.WithTMDB(client)`; same shape will apply to Prowlarr and
  qBittorrent when those tests come online. Never swap a service pointer
  after construction — downstream consumers built inside `service.New` hold
  references to the original. The seam belongs in `service.New` itself.
- **Authenticated by default.** `app.GET` / `POST` / `PUT` / `DELETE` always
  set the admin Bearer token. For unauthenticated-path tests, build the
  request manually (see `testapp_smoke_test.go:TestApp_Unauthenticated`).
- **Status mismatches fail the test.** `app.Do` asserts on the expected
  status code; the `doRaw` helper in `libraries_test.go` is for cases where
  the test wants the body bytes for `apperrors.ProblemDetails` decoding
  regardless of status assertion order.
